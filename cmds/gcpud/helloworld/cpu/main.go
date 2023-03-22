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
	"time"

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

// fake does fake listens. It returns whatever is in the chan.
// it has to be done this way such that it will block the goroutine
// doing the listen.
type fake struct {
	conn chan net.Conn
}

func (f *fake) Accept() (net.Conn, error) {
	log.Printf("ACCEPT:")
	//fc := NewfakeConn(<-f.conn)
	fc := <-f.conn
	log.Printf("gets %v", fc)
	return fc, nil
}

func (f*fake) Close() error {
	log.Panicf("fakelisterer Close %T %v", f, f)
	return nil
}

func (f *fake) Addr() net.Addr {
	log.Printf("ADDR %v", net.TCPAddr{})
	return &net.TCPAddr{}
}

// This is annoying, but grpc is painful to debug
type fakeConn struct {
	Conn net.Conn
}

func NewfakeConn(carrier net.Conn) *fakeConn {
	return &fakeConn{
		Conn: carrier,
	}
}

var tot int
func (c *fakeConn) Read(b []byte) (n int, err error) {
	log.Printf("connread")
	n, err = c.Conn.Read(b)
	tot += n
	log.Printf("connread: %d %d so far, %#x(%q), %v", n, tot, b[:n], b[:n], err)
	return n, err
}

func (c *fakeConn) Write(b []byte) (n int, err error) {
	log.Printf("connWrite")
	return c.Conn.Write(b)
}

func (c *fakeConn) Close() error {
	log.Panicf("Close %T %T %v", c, c.Conn, c.Conn)
	return c.Conn.Close()
}

func (c *fakeConn) LocalAddr() net.Addr {
	log.Printf("connLocalddr %v", c.Conn.LocalAddr())
	return c.Conn.LocalAddr()
}

func (c *fakeConn) RemoteAddr() net.Addr {
	log.Printf("connRemoteddr %v", c.Conn.RemoteAddr())
	return c.Conn.RemoteAddr()
}

func (c *fakeConn) SetDeadline(t time.Time) error {
	log.Printf("connsetdeadline %v", t)
	return c.Conn.SetDeadline(t)
}

func (c *fakeConn) SetReadDeadline(t time.Time) error {
	log.Printf("connsetReaddeadline %v", t)
	return c.Conn.SetReadDeadline(t)
}

func (c *fakeConn) SetWriteDeadline(t time.Time) error {
	log.Printf("connsetWritedeadline %v", t)
	return c.Conn.SetWriteDeadline(t)
}

// Static checking for type
var _ net.Conn = &fakeConn{}

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
	fchan := make(chan net.Conn, 1)
	fchan <- conn
	lis := &fake{conn: fchan}
	if err := s.Serve(lis); err != nil {
		log.Fatalf("failed to serve: %v", err)
	}
}
