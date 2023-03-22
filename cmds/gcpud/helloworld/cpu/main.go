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

//go:generate protoc --go_out=. --go_opt=paths=source_relative --go-grpc_out=. --go-grpc_opt=paths=source_relative helloworld/helloworld.proto
// Package main implements a server for Greeter service.
package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"log"
	"net"
	"os"

	pb "github.com/u-root/cpu/cmds/gcpud/helloworld/helloworld"
	"google.golang.org/grpc"
)

var (
	addr = flag.String("addr", ":6666", "The server addr")
)

// server is used to implement helloworld.GreeterServer.
type server struct {
	pb.UnimplementedGreeterServer
	r   io.Reader
	err error
}

// SayHello implements helloworld.GreeterServer
func (s *server) SayHello(ctx context.Context, in *pb.HelloRequest) (*pb.HelloReply, error) {
	out := in.GetName()
	fmt.Printf("%s", out)
	var r = &pb.HelloReply{}
	return r, nil
}

// SayHello implements helloworld.GreeterServer
func (s *server) Stdin(ctx context.Context, in *pb.HelloRequest) (*pb.HelloReply, error) {
	var r = &pb.HelloReply{}
	var data [512]byte
	n, err := s.r.Read(data[:])
	r.Message = data[:n]
	return r, err
}

type fake struct {
	conn net.Conn
}

func (f *fake) Accept() (net.Conn, error) {
	return f.conn, nil
}

func (*fake) Close() error {
	return nil
}

func (f *fake) Addr() net.Addr {
	return f.conn.RemoteAddr()
}

func main() {
	flag.Parse()
	// Dial the server and then use the socket.
	// So, in a sense, we change from client to server.
	conn, err := net.Dial("tcp", *addr)
	if err != nil {
		log.Fatal(err)
	}
	s := grpc.NewServer()
	pb.RegisterGreeterServer(s, &server{r: os.Stdin})
	lis := &fake{conn: conn}
	log.Printf("server listening at %v", lis.Addr())
	if err := s.Serve(lis); err != nil {
		log.Fatalf("failed to serve: %v", err)
	}
}
