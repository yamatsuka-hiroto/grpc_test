package main

import (
	"fmt"
	"log"
	"net"
	"os"
	_ "strconv"
	"sync"
	"time"

	"github.com/wayn3h0/go-uuid/internal/random"
	pb "github.com/yamatsuka-hiroto/grpc_test/proto"
	"golang.org/x/net/context"
	"google.golang.org/grpc"
)

type KVStoreGRPC struct {
	mu    sync.Mutex
	store map[string][]byte
}

var (
	connsN   = 30
	clientsN = 100
	reqN     = 100000

	//	network = "tcp"
	//	address = ":5000"
	network = "unix"
	address = "/tmp/grpc_test.sock"

	keys = make([][]byte, reqN)
	vals = make([][]byte, reqN)
)

// データ作成
func init() {
	for i := 0; i < reqN; i++ {
		keys[i], _ = random.New()
		vals[i], _ = random.New()
	}
}

func main() {
	defer func() {
		if err := os.Remove(address); err != nil {
			fmt.Println(err)
		}
	}()

	// gRPC サーバーの起動
	startServerGRPC(network, address)

	// connection 数をインクリメントして検証する
	for i := 1; i < connsN+1; i++ {
		Stress(network, address, keys, vals, i, clientsN)
	}
}

func (s *KVStoreGRPC) Put(ctx context.Context, r *pb.PutRequest) (*pb.PutResponse, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	resp := &pb.PutResponse{}
	resp.Header = &pb.ResponseHeader{}
	if v, ok := s.store[string(r.Key)]; ok {
		resp.Header.Exist = true
		resp.Header.Value = v
	} else {
		s.store[string(r.Key)] = r.Value
	}
	return resp, nil
}

func startServerGRPC(network, port string) {
	ln, err := net.Listen(network, port)
	if err != nil {
		panic(err)
	}

	s := &KVStoreGRPC{}
	s.store = make(map[string][]byte)

	grpcServer := grpc.NewServer()
	pb.RegisterKVServer(grpcServer, s)
	go func() {
		if err := grpcServer.Serve(ln); err != nil {
			panic(err)
		}
	}()
}

func genConn(network, address string) (*grpc.ClientConn, error) {
	switch network {
	case "tcp":
		return grpc.Dial(address, grpc.WithInsecure())

	case "unix":
		dialer := func(a string, t time.Duration) (net.Conn, error) {
			return net.Dial(network, a)
		}
		return grpc.Dial(address, grpc.WithInsecure(), grpc.WithDialer(dialer))
	default:
		panic("invalid network")
	}
}

func Stress(network, address string, keys, vals [][]byte, connsN, clientsN int) {
	conns := make([]*grpc.ClientConn, connsN)
	for i := range conns {
		conn, err := genConn(network, address)
		if err != nil {
			panic(err)
		}
		conns[i] = conn
	}
	// client の作成
	clients := make([]pb.KVClient, clientsN)
	for i := range clients {
		clients[i] = pb.NewKVClient(conns[i%int(connsN)])
	}

	requests := make(chan *pb.PutRequest, len(keys))
	done, errChan := make(chan struct{}), make(chan error)

	for i := range clients {
		go func(i int, requests chan *pb.PutRequest) {
			for r := range requests {
				if _, err := clients[i].Put(context.Background(), r); err != nil {
					errChan <- err
					return
				}
			}
			done <- struct{}{}
		}(i, requests)
	}

	st := time.Now()

	for i := range keys {
		r := &pb.PutRequest{
			Key:   keys[i],
			Value: vals[i],
		}
		requests <- r
	}

	close(requests)

	cn := 0
	for cn != len(clients) {
		select {
		case err := <-errChan:
			panic(err)
		case <-done:
			cn++
		}
	}
	close(done)
	close(errChan)

	tt := time.Since(st)
	size := len(keys)
	pt := tt / time.Duration(size)
	// log.Printf("conn:%d total:%v requests:%d client(s):%d (%v per each).\n", connsN, tt, size, clientsN, pt)
	log.Printf("%v, %v", tt, pt)
}
