package main

//go:generate protoc --go_out=. --go_opt=paths=source_relative --go-grpc_out=. --go-grpc_opt=paths=source_relative cpu.proto

import (
	"fmt"
	"log"
	"net"
	"os/exec"
	"sync"

	pb "github.com/pramonow/go-grpc-server-streaming-example/src/proto"

	"google.golang.org/grpc"
)

type server struct{}

func (s server) FetchResponse(in *pb.Request, srv pb.StreamService_FetchResponseServer) error {

	log.Printf("fetch response for id : %d", in.Id)

	c := exec.Command("bash")
	var err error
	stdin, err := c.StdinPipe()
	if err != nil {
		return err
	}
	stdout, err := c.StdoutPipe()
	if err != nil {
		return err
	}
	stderr, err := c.StderrPipe()
	if err != nil {
		return err
	}

	var wg sync.WaitGroup
	{
		wg.Add(2)
		go func() {
			defer wg.Done()
			for {
				var b [1]byte
				if _, err := stdout.Read(b[:]); err != nil {
					log.Printf("ou error %v", err)
					return
				}
				resp := pb.Response{Result: fmt.Sprintf("%c %v", b[0], err)}
				if err := srv.Send(&resp); err != nil {
					log.Printf("send error %v", err)
					return
				}
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
				resp := pb.Response{Result: fmt.Sprintf("%c %v", b[0], err)}
				if err := srv.Send(&resp); err != nil {
					log.Printf("send error %v", err)
					return
				}
			}
		}()
	}
	if _, err := stdin.Write([]byte("date\n")); err != nil {
		log.Printf("write command: %v", err)
	}
	if err := c.Run(); err != nil {
		log.Printf("run: %v", err)
	}
	wg.Wait()
	return nil
}

func main() {
	// create listiner
	lis, err := net.Listen("tcp", ":50005")
	if err != nil {
		log.Fatalf("failed to listen: %v", err)
	}

	// create grpc server
	s := grpc.NewServer()
	pb.RegisterStreamServiceServer(s, server{})

	log.Println("start server")
	// and start...
	if err := s.Serve(lis); err != nil {
		log.Fatalf("failed to serve: %v", err)
	}

}
