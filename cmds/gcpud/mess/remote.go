package main

//go:generate protoc --go_out=. --go_opt=paths=source_relative --go-grpc_out=. --go-grpc_opt=paths=source_relative cpu/cpu.proto

import (
	"context"
	"fmt"
	"io"
	"log"
	"math/rand"
	"os/exec"
	"sync"
	"time"

	pb "github.com/u-root/cpu/cmds/gcpud/mess/cpu"

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
	rand.Seed(time.Now().Unix())

	// dail server
	conn, err := grpc.Dial("localhost:50005", grpc.WithInsecure())
	if err != nil {
		log.Fatalf("can not connect with server %v", err)
	}

	// create stream
	client := pb.NewStreamServiceClient(conn)
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

	<-done
	log.Printf("finished")
}
