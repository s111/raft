// DO NOT EDIT. Generated by 'gorums' plugin for protoc-gen-go
// Source file to edit is: mgr_rpc_tmpl

package dev

import (
	"time"

	"golang.org/x/net/context"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
)

type readReply struct {
	nid   uint32
	reply *State
	err   error
}

func (m *Manager) read(c *Configuration, args *ReadRequest) (*ReadReply, error) {
	replyChan := make(chan readReply, c.n)
	ctx, cancel := context.WithCancel(context.Background())

	for _, n := range c.nodes {
		go callGRPCRead(ctx, n, args, replyChan)
	}

	var (
		replyValues = make([]*State, 0, c.n)
		reply       = &ReadReply{NodeIDs: make([]uint32, 0, c.n)}
		errCount    int
		quorum      bool
	)

	for {
		select {
		case r := <-replyChan:
			if r.err != nil {
				errCount++
				goto terminationCheck
			}
			replyValues = append(replyValues, r.reply)
			reply.NodeIDs = append(reply.NodeIDs, r.nid)
			if reply.Reply, quorum = c.qspec.ReadQF(replyValues); quorum {
				cancel()
				return reply, nil
			}
		case <-time.After(c.timeout):
			cancel()
			return reply, TimeoutRPCError{c.timeout, errCount, len(replyValues)}
		}

	terminationCheck:
		if errCount+len(replyValues) == c.n {
			cancel()
			return reply, IncompleteRPCError{errCount, len(replyValues)}
		}
	}
}

func callGRPCRead(ctx context.Context, node *Node, args *ReadRequest, replyChan chan<- readReply) {
	reply := new(State)
	start := time.Now()
	err := grpc.Invoke(
		ctx,
		"/dev.Register/Read",
		args,
		reply,
		node.conn,
	)
	switch grpc.Code(err) { // nil -> codes.OK
	case codes.OK, codes.Canceled:
		node.setLatency(time.Since(start))
	default:
		node.setLastErr(err)
	}
	replyChan <- readReply{node.id, reply, err}
}

type writeReply struct {
	nid   uint32
	reply *WriteResponse
	err   error
}

func (m *Manager) write(c *Configuration, args *State) (*WriteReply, error) {
	replyChan := make(chan writeReply, c.n)
	ctx, cancel := context.WithCancel(context.Background())

	for _, n := range c.nodes {
		go callGRPCWrite(ctx, n, args, replyChan)
	}

	var (
		replyValues = make([]*WriteResponse, 0, c.n)
		reply       = &WriteReply{NodeIDs: make([]uint32, 0, c.n)}
		errCount    int
		quorum      bool
	)

	for {
		select {
		case r := <-replyChan:
			if r.err != nil {
				errCount++
				goto terminationCheck
			}
			replyValues = append(replyValues, r.reply)
			reply.NodeIDs = append(reply.NodeIDs, r.nid)
			if reply.Reply, quorum = c.qspec.WriteQF(replyValues); quorum {
				cancel()
				return reply, nil
			}
		case <-time.After(c.timeout):
			cancel()
			return reply, TimeoutRPCError{c.timeout, errCount, len(replyValues)}
		}

	terminationCheck:
		if errCount+len(replyValues) == c.n {
			cancel()
			return reply, IncompleteRPCError{errCount, len(replyValues)}
		}
	}
}

func callGRPCWrite(ctx context.Context, node *Node, args *State, replyChan chan<- writeReply) {
	reply := new(WriteResponse)
	start := time.Now()
	err := grpc.Invoke(
		ctx,
		"/dev.Register/Write",
		args,
		reply,
		node.conn,
	)
	switch grpc.Code(err) { // nil -> codes.OK
	case codes.OK, codes.Canceled:
		node.setLatency(time.Since(start))
	default:
		node.setLastErr(err)
	}
	replyChan <- writeReply{node.id, reply, err}
}

func (m *Manager) writeAsync(c *Configuration, args *State) error {
	for _, node := range c.nodes {
		go func(n *Node) {
			err := n.WriteAsyncClient.Send(args)
			if err == nil {
				return
			}
			if m.logger != nil {
				m.logger.Printf("%d: writeAsync stream send error: %v", n.id, err)
			}
		}(node)
	}

	return nil
}
