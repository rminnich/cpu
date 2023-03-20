package main

//go:generate protoc --go_out=. --go_opt=paths=source_relative --go-grpc_out=. --go-grpc_opt=paths=source_relative cpu.proto

import (
	"context"
	"io"
	"log"
	"net"
	"sync"

	"pb"

	"google.golang.org/grpc"
)

type server struct{}

func (s server) FetchResponse(in *pb.Request, srv pb.StreamService_FetchResponseServer) error {

	log.Printf("fetch response for id : %d", in.Id)

	var wg sync.WaitGroup
	{
		wg.Add(2)
		in := &pb.Request{Id: 1}
		stream, err := client.FetchResponse(context.Background(), in)
		if err != nil {
			log.Fatalf("openn stream error %v", err)
		}

		//ctx := stream.Context()
		done := make(chan bool)

		go func() {
			for {
				resp, err := stream.Recv()
				if err == io.EOF {
					done <- true //close(done)
					return
				}
				if err != nil {
					log.Fatalf("can not receive %v", err)
				}
				log.Printf("Resp received: %s", resp.Result)
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
