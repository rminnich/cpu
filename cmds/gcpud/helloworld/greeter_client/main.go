/*
 *
 * Copyright 2015 gRPC authors.
 *
 * Licensed under the Apache License, Version 2.0 (the "License");
 * you may not use this file except in compliance with the License.
 * You may obtain a copy of the License at
 *
 *     http://www.apache.org/licenses/LICENSE-2.0
 *
 * Unless required by applicable law or agreed to in writing, software
 * distributed under the License is distributed on an "AS IS" BASIS,
 * WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
 * See the License for the specific language governing permissions and
 * limitations under the License.
 *
 */

// Package main implements a client for Greeter service.
package main

import (
	"context"
	"flag"
	"log"
	"os/exec"
	"sync"
	"time"

	pb "github.com/u-root/cpu/cmds/gcpud/helloworld/helloworld"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

const (
	defaultName = "world"
)

var (
	addr = flag.String("addr", "localhost:50051", "the address to connect to")
	name = flag.String("name", defaultName, "Name to greet")
)

func main() {
	flag.Parse()
	// Set up a connection to the server.
	conn, err := grpc.Dial(*addr, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		log.Fatalf("did not connect: %v", err)
	}
	defer conn.Close()
	c := pb.NewGreeterClient(conn)

	cmd := exec.Command("bash")

	stdin, err := cmd.StdinPipe()
	if err != nil {
		return
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		return
	}

	var wg sync.WaitGroup
	{
		wg.Add(3)
		go func() {
			defer wg.Done()
			for {
				ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
				defer cancel()
				r, err := c.Stdin(ctx, &pb.HelloRequest{})
				if err != nil {
					log.Printf("could not greet: %v, %v", r, err)
					return
				}
				s := r.GetMessage()
				log.Printf("stdin: %q", s)
				if _, err := stdin.Write([]byte(s)); err != nil {
					log.Printf("Writing stdin: %v", err)
					return
				}
			}
		}()
		go func() {
			defer wg.Done()
			for {
				var b [1]byte
				if _, err := stdout.Read(b[:]); err != nil {
					log.Printf("ou error %v", err)
					return
				}
				ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
				defer cancel()
				r, err := c.SayHello(ctx, &pb.HelloRequest{Name: string(b[:])})
				if err != nil {
					log.Printf("could not greet: %v, %v", r, err)
					return
				}
				log.Printf("Greeting: %s", r.GetMessage())
			}
		}()
		go func() {
			defer wg.Done()
			for {
				var b [1]byte
				if _, err := stderr.Read(b[:]); err != nil {
					log.Printf("ou error %v", err)
					return
				}
				ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
				defer cancel()
				r, err := c.SayHello(ctx, &pb.HelloRequest{Name: string(b[:])})
				if err != nil {
					log.Printf("could not greet: %v, %v", r, err)
					return
				}
			}
		}()
	}

	if err := cmd.Run(); err != nil {
		log.Printf("run: %v", err)
	}
}
