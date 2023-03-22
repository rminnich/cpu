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
	"net"
	"os"
	"sync"
	"time"

	pb "github.com/u-root/cpu/cmds/gcpud/helloworld/helloworld"
	"github.com/u-root/cpu/session"
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

var (
	// For the ssh server part
	hostKeyFile = flag.String("hk", "" /*"/etc/ssh/ssh_host_rsa_key"*/, "file for host key")
	pubKeyFile  = flag.String("pk", "key.pub", "file for public key")
	port        = flag.String("sp", ":6666", "cpu default port")

	debug     = flag.Bool("d", false, "enable debug prints")
	runAsInit = flag.Bool("init", false, "run as init (Debug only; normal test is if we are pid 1")
	// v allows debug printing.
	// Do not call it directly, call verbose instead.
	v       = func(string, ...interface{}) {}
	remote  = flag.Bool("remote", false, "indicates we are the remote side of the cpu session")
	network = flag.String("net", "tcp", "network to use")
	port9p  = flag.String("port9p", "", "port9p # on remote machine for 9p mount")
	klog    = flag.Bool("klog", false, "Log cpud messages in kernel log, not stdout")

	// Some networks are not well behaved, and for them we implement registration.
	registerAddr = flag.String("register", "", "address and port to register with after listen on cpu server port")
	registerTO   = flag.Duration("registerTO", time.Duration(5*time.Second), "time.Duration for Dial address for registering")

	pid1 bool
)

func verbose(f string, a ...interface{}) {
	if *remote {
		v("CPUD(remote):"+f+"\r\n", a...)
	} else {
		v("CPUD:"+f, a...)
	}
}

// This is most of cpud with things disabled for now. The assumption is
// we are started, do an accept, and the connect back on the socket
// we accepted on. So in some sense we "switch modes" from serving (doing
// the accept) to being a client.
func main() {
	flag.Parse()

	lis, err := net.Listen("tcp", *port)
	if err != nil {
		log.Fatalf("failed to listen: %v", err)
	}
	conn, err := lis.Accept()
	if err != nil {
		log.Fatalf("accept: %v", err)
	}

	gc, err := grpc.Dial(*addr, grpc.WithTransportCredentials(insecure.NewCredentials()), grpc.WithDialer(func(string, time.Duration) (net.Conn, error) {
		return conn, nil
	}))
	if err != nil {
		log.Fatal(err)
	}

	c := pb.NewGreeterClient(gc)

	*remote = true
	if *remote {
		if *debug {
			v = log.Printf
			session.SetVerbose(verbose)
		}
	} else {
		/* not now
		if err := commonsetup(); err != nil {
			log.Fatal(err)
		}
		*/
	}
	pid1 = os.Getpid() == 1
	*runAsInit = *runAsInit || pid1
	verbose("Args %v pid %d *runasinit %v *remote %v env %v", os.Args, os.Getpid(), *runAsInit, *remote, os.Environ())
	args := flag.Args()
	if *remote {
		verbose("args %q, port9p %v", args, *port9p)
		tmpMnt, ok := os.LookupEnv("CPU_TMPMNT")
		if !ok || len(tmpMnt) == 0 {
			tmpMnt = "/tmp"
		}
		s := session.New(*port9p, tmpMnt, args[0], args[1:]...)
		inout, sinout := net.Pipe()
		s.Stdin, s.Stdout = sinout, sinout
		stderr, sstderr := net.Pipe()
		s.Stderr = sstderr
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
					msg := r.GetMessage()
					log.Printf("stdin: %q", msg)
					if _, err := inout.Write([]byte(msg)); err != nil {
						log.Printf("Writing stdin: %v", err)
						return
					}
				}
			}()
			go func() {
				defer wg.Done()
				for {
					var b [4096]byte
					n, err := inout.Read(b[:])
					if err != nil {
						log.Printf("ou error %v", err)
						return
					}
					ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
					defer cancel()
					r, err := c.SayHello(ctx, &pb.HelloRequest{Name: b[:n]})
					if err != nil {
						log.Printf("could not greet: %v, %v", r, err)
						return
					}
				}
			}()
			go func() {
				defer wg.Done()
				for {
					var b [1]byte
					n, err := stderr.Read(b[:])
					if err != nil {
						log.Printf("ou error %v", err)
						return
					}
					ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
					defer cancel()
					r, err := c.SayHello(ctx, &pb.HelloRequest{Name: b[:n]})
					if err != nil {
						log.Printf("could not greet: %v, %v", r, err)
						return
					}
				}
			}()
		}

		if err := s.Run(); err != nil {
			log.Fatalf("CPUD(remote): %v", err)
		}
	} else {
		log.Fatalf("CPUD:running as a server (a.k.a. starter of cpud's for sessions)")
		if *runAsInit {
			// if err := initsetup(); err != nil {
			// 	log.Fatal(err)
			// }
		}
		// if err := serve(); err != nil {
		// 	log.Fatal(err)
		// }
	}
	log.Printf("all done")
}
