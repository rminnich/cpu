package main

import (
	"context"
	"io"
	"log"
	"math/rand"

	pb "github.com/pramonow/go-grpc-server-streaming-example/src/proto"

	"time"

	"google.golang.org/grpc"
)

func main() {
	rand.Seed(time.Now().Unix())

	// dail server
	conn, err := grpc.Dial("localhost:50005", grpc.WithInsecure())
	if err != nil {
		log.Fatalf("can not connect with server %v", err)
	}

	// create stream
	iter := 1
	client := pb.NewStreamServiceClient(conn)
	in := &pb.Request{Id: iter}
	iter++
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

	go func() {
		const sleep = 5 * time.Second
		if err := time.Sleep(sleep); err != nil {
			log.Printf("Sleep %v:%v", sleep, err)
			return
		}
		in := &pb.Request{Id: iter, Stdin: "hi"}
		iter++
		stream, err := client.FetchResponse(context.Background(), in)
	}()

	<-done
	log.Printf("finished")
}
