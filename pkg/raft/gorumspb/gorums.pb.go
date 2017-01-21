// Code generated by protoc-gen-gogo.
// source: pkg/raft/gorumspb/gorums.proto
// DO NOT EDIT!

/*
	Package gorumspb is a generated protocol buffer package.

	It is generated from these files:
		pkg/raft/gorumspb/gorums.proto

	It has these top-level messages:
*/
package gorumspb

import proto "github.com/gogo/protobuf/proto"
import fmt "fmt"
import math "math"
import _ "github.com/relab/gorums"
import raftpb "github.com/relab/raft/pkg/raft/raftpb"

import (
	"bytes"
	"encoding/binary"
	"hash/fnv"
	"io"
	"log"
	"net"
	"sort"
	"strings"
	"sync"
	"time"

	"golang.org/x/net/trace"

	"google.golang.org/grpc/codes"
)

import (
	context "golang.org/x/net/context"
	grpc "google.golang.org/grpc"
)

// Reference imports to suppress errors if they are not otherwise used.
var _ = proto.Marshal
var _ = fmt.Errorf
var _ = math.Inf

// This is a compile-time assertion to ensure that this generated file
// is compatible with the proto package it is being compiled against.
// A compilation error at this line likely means your copy of the
// proto package needs to be updated.
const _ = proto.GoGoProtoPackageIsVersion2 // please upgrade the proto package

//  Reference Gorums specific imports to suppress errors if they are not otherwise used.
var _ = codes.OK

/* 'gorums' plugin for protoc-gen-go - generated from: config_qc_tmpl */

// AppendEntriesReply encapsulates the reply from a AppendEntries quorum call.
// It contains the id of each node of the quorum that replied and a single reply.
type AppendEntriesReply struct {
	NodeIDs []uint32
	*raftpb.AppendEntriesResponse
}

func (r AppendEntriesReply) String() string {
	return fmt.Sprintf("node ids: %v | answer: %v", r.NodeIDs, r.AppendEntriesResponse)
}

// AppendEntries invokes a AppendEntries quorum call on configuration c
// and returns the result as a AppendEntriesReply.
func (c *Configuration) AppendEntries(ctx context.Context, args *raftpb.AppendEntriesRequest) (*AppendEntriesReply, error) {
	return c.mgr.appendEntries(ctx, c, args)
}

// RequestVoteReply encapsulates the reply from a RequestVote quorum call.
// It contains the id of each node of the quorum that replied and a single reply.
type RequestVoteReply struct {
	NodeIDs []uint32
	*raftpb.RequestVoteResponse
}

func (r RequestVoteReply) String() string {
	return fmt.Sprintf("node ids: %v | answer: %v", r.NodeIDs, r.RequestVoteResponse)
}

// RequestVoteFuture is a reference to an asynchronous RequestVote quorum call invocation.
type RequestVoteFuture struct {
	reply *RequestVoteReply
	err   error
	c     chan struct{}
}

// RequestVoteFuture asynchronously invokes a RequestVote quorum call
// on configuration c and returns a RequestVoteFuture which can be used to
// inspect the quorum call reply and error when available.
func (c *Configuration) RequestVoteFuture(ctx context.Context, args *raftpb.RequestVoteRequest) *RequestVoteFuture {
	f := new(RequestVoteFuture)
	f.c = make(chan struct{}, 1)
	go func() {
		defer close(f.c)
		f.reply, f.err = c.mgr.requestVote(ctx, c, args)
	}()
	return f
}

// Get returns the reply and any error associated with the RequestVoteFuture.
// The method blocks until a reply or error is available.
func (f *RequestVoteFuture) Get() (*RequestVoteReply, error) {
	<-f.c
	return f.reply, f.err
}

// Done reports if a reply and/or error is available for the RequestVoteFuture.
func (f *RequestVoteFuture) Done() bool {
	select {
	case <-f.c:
		return true
	default:
		return false
	}
}

/* 'gorums' plugin for protoc-gen-go - generated from: mgr_qc_tmpl */

type appendEntriesReply struct {
	nid   uint32
	reply *raftpb.AppendEntriesResponse
	err   error
}

func (m *Manager) appendEntries(ctx context.Context, c *Configuration, args *raftpb.AppendEntriesRequest) (r *AppendEntriesReply, err error) {
	var ti traceInfo
	if m.opts.trace {
		ti.tr = trace.New("gorums."+c.tstring()+".Sent", "AppendEntries")
		defer ti.tr.Finish()

		ti.firstLine.cid = c.id
		if deadline, ok := ctx.Deadline(); ok {
			ti.firstLine.deadline = deadline.Sub(time.Now())
		}
		ti.tr.LazyLog(&ti.firstLine, false)

		defer func() {
			ti.tr.LazyLog(&qcresult{
				ids:   r.NodeIDs,
				reply: r.AppendEntriesResponse,
				err:   err,
			}, false)
			if err != nil {
				ti.tr.SetError()
			}
		}()
	}

	replyChan := make(chan appendEntriesReply, c.n)
	newCtx, cancel := context.WithCancel(ctx)

	if m.opts.trace {
		ti.tr.LazyLog(&payload{sent: true, msg: args}, false)
	}

	for _, n := range c.nodes {
		go callGRPCAppendEntries(newCtx, n, args, replyChan)
	}

	var (
		replyValues = make([]*raftpb.AppendEntriesResponse, 0, c.n)
		reply       = &AppendEntriesReply{NodeIDs: make([]uint32, 0, c.n)}
		errCount    int
		quorum      bool
	)

	for {
		select {
		case r := <-replyChan:
			reply.NodeIDs = append(reply.NodeIDs, r.nid)
			if r.err != nil {
				errCount++
				break
			}
			if m.opts.trace {
				ti.tr.LazyLog(&payload{sent: false, id: r.nid, msg: r.reply}, false)
			}
			replyValues = append(replyValues, r.reply)
			if reply.AppendEntriesResponse, quorum = c.qspec.AppendEntriesQF(args, replyValues); quorum {

				return reply, nil
			}
		case <-newCtx.Done():
			return reply, QuorumCallError{ctx.Err().Error(), errCount, len(replyValues)}
		}

		if errCount+len(replyValues) == c.n {
			cancel()
			return reply, QuorumCallError{"incomplete call", errCount, len(replyValues)}
		}
	}
}

func callGRPCAppendEntries(ctx context.Context, node *Node, args *raftpb.AppendEntriesRequest, replyChan chan<- appendEntriesReply) {
	reply := new(raftpb.AppendEntriesResponse)
	start := time.Now()
	err := grpc.Invoke(
		ctx,
		"/gorumspb.Raft/AppendEntries",
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
	replyChan <- appendEntriesReply{node.id, reply, err}
}

type requestVoteReply struct {
	nid   uint32
	reply *raftpb.RequestVoteResponse
	err   error
}

func (m *Manager) requestVote(ctx context.Context, c *Configuration, args *raftpb.RequestVoteRequest) (r *RequestVoteReply, err error) {
	var ti traceInfo
	if m.opts.trace {
		ti.tr = trace.New("gorums."+c.tstring()+".Sent", "RequestVote")
		defer ti.tr.Finish()

		ti.firstLine.cid = c.id
		if deadline, ok := ctx.Deadline(); ok {
			ti.firstLine.deadline = deadline.Sub(time.Now())
		}
		ti.tr.LazyLog(&ti.firstLine, false)

		defer func() {
			ti.tr.LazyLog(&qcresult{
				ids:   r.NodeIDs,
				reply: r.RequestVoteResponse,
				err:   err,
			}, false)
			if err != nil {
				ti.tr.SetError()
			}
		}()
	}

	replyChan := make(chan requestVoteReply, c.n)
	newCtx, cancel := context.WithCancel(ctx)

	if m.opts.trace {
		ti.tr.LazyLog(&payload{sent: true, msg: args}, false)
	}

	for _, n := range c.nodes {
		go callGRPCRequestVote(newCtx, n, args, replyChan)
	}

	var (
		replyValues = make([]*raftpb.RequestVoteResponse, 0, c.n)
		reply       = &RequestVoteReply{NodeIDs: make([]uint32, 0, c.n)}
		errCount    int
		quorum      bool
	)

	for {
		select {
		case r := <-replyChan:
			reply.NodeIDs = append(reply.NodeIDs, r.nid)
			if r.err != nil {
				errCount++
				break
			}
			if m.opts.trace {
				ti.tr.LazyLog(&payload{sent: false, id: r.nid, msg: r.reply}, false)
			}
			replyValues = append(replyValues, r.reply)
			if reply.RequestVoteResponse, quorum = c.qspec.RequestVoteQF(args, replyValues); quorum {

				return reply, nil
			}
		case <-newCtx.Done():
			return reply, QuorumCallError{ctx.Err().Error(), errCount, len(replyValues)}
		}

		if errCount+len(replyValues) == c.n {
			cancel()
			return reply, QuorumCallError{"incomplete call", errCount, len(replyValues)}
		}
	}
}

func callGRPCRequestVote(ctx context.Context, node *Node, args *raftpb.RequestVoteRequest, replyChan chan<- requestVoteReply) {
	reply := new(raftpb.RequestVoteResponse)
	start := time.Now()
	err := grpc.Invoke(
		ctx,
		"/gorumspb.Raft/RequestVote",
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
	replyChan <- requestVoteReply{node.id, reply, err}
}

/* 'gorums' plugin for protoc-gen-go - generated from: node_tmpl */

// Node encapsulates the state of a node on which a remote procedure call
// can be made.
type Node struct {
	// Only assigned at creation.
	id   uint32
	self bool
	addr string
	conn *grpc.ClientConn

	RaftClient RaftClient

	sync.Mutex
	lastErr error
	latency time.Duration
}

func (n *Node) connect(opts ...grpc.DialOption) error {
	var err error
	n.conn, err = grpc.Dial(n.addr, opts...)
	if err != nil {
		return fmt.Errorf("dialing node failed: %v", err)
	}

	n.RaftClient = NewRaftClient(n.conn)

	return nil
}

func (n *Node) close() error {
	// TODO: Log error, mainly care about the connection error below.
	// We should log this error, but we currently don't have access to the
	// logger in the manager.

	if err := n.conn.Close(); err != nil {
		return fmt.Errorf("conn close error: %v", err)
	}
	return nil
}

/* 'gorums' plugin for protoc-gen-go - generated from: qspec_tmpl */

// QuorumSpec is the interface that wraps every quorum function.
type QuorumSpec interface {
	// AppendEntriesQF is the quorum function for the AppendEntries
	// quorum call method.
	AppendEntriesQF(req *raftpb.AppendEntriesRequest, replies []*raftpb.AppendEntriesResponse) (*raftpb.AppendEntriesResponse, bool)

	// RequestVoteQF is the quorum function for the RequestVote
	// quorum call method.
	RequestVoteQF(req *raftpb.RequestVoteRequest, replies []*raftpb.RequestVoteResponse) (*raftpb.RequestVoteResponse, bool)
}

/* Static resources */

/* config.go */

// A Configuration represents a static set of nodes on which quorum remote
// procedure calls may be invoked.
type Configuration struct {
	id    uint32
	nodes []*Node
	n     int
	mgr   *Manager
	qspec QuorumSpec
}

// ID reports the identifier for the configuration.
func (c *Configuration) ID() uint32 {
	return c.id
}

// NodeIDs returns a slice containing the local ids of all the nodes in the
// configuration. IDs are returned in the same order as they were provided in
// the creation of the Configuration.
func (c *Configuration) NodeIDs() []uint32 {
	ids := make([]uint32, len(c.nodes))
	for i, node := range c.nodes {
		ids[i] = node.ID()
	}
	return ids
}

// Nodes returns a slice of each available node. IDs are returned in the same
// order as they were provided in the creation of the Configuration.
func (c *Configuration) Nodes() []*Node {
	return c.nodes
}

// Size returns the number of nodes in the configuration.
func (c *Configuration) Size() int {
	return c.n
}

func (c *Configuration) String() string {
	return fmt.Sprintf("configuration %d", c.id)
}

func (c *Configuration) tstring() string {
	return fmt.Sprintf("config-%d", c.id)
}

// Equal returns a boolean reporting whether a and b represents the same
// configuration.
func Equal(a, b *Configuration) bool { return a.id == b.id }

// NewTestConfiguration returns a new configuration with quorum size q and
// node size n. No other fields are set. Configurations returned from this
// constructor should only be used when testing quorum functions.
func NewTestConfiguration(q, n int) *Configuration {
	return &Configuration{
		nodes: make([]*Node, n),
	}
}

/* errors.go */

// A NodeNotFoundError reports that a specified node could not be found.
type NodeNotFoundError uint32

func (e NodeNotFoundError) Error() string {
	return fmt.Sprintf("node not found: %d", e)
}

// A ConfigNotFoundError reports that a specified configuration could not be
// found.
type ConfigNotFoundError uint32

func (e ConfigNotFoundError) Error() string {
	return fmt.Sprintf("configuration not found: %d", e)
}

// An IllegalConfigError reports that a specified configuration could not be
// created.
type IllegalConfigError string

func (e IllegalConfigError) Error() string {
	return "illegal configuration: " + string(e)
}

// ManagerCreationError returns an error reporting that a Manager could not be
// created due to err.
func ManagerCreationError(err error) error {
	return fmt.Errorf("could not create manager: %s", err.Error())
}

// A QuorumCallError is used to report that a quorum call failed.
type QuorumCallError struct {
	Reason               string
	ErrCount, ReplyCount int
}

func (e QuorumCallError) Error() string {
	return fmt.Sprintf(
		"quorum call error: %s (errors: %d, replies: %d)",
		e.Reason, e.ErrCount, e.ReplyCount,
	)
}

/* level.go */

// LevelNotSet is the zero value level used to indicate that no level (and
// thereby no reply) has been set for a correctable quorum call.
const LevelNotSet = -1

/* mgr.go */

// Manager manages a pool of node configurations on which quorum remote
// procedure calls can be made.
type Manager struct {
	sync.Mutex
	nodes    []*Node
	lookup   map[uint32]*Node
	configs  map[uint32]*Configuration
	eventLog trace.EventLog

	closeOnce sync.Once
	logger    *log.Logger
	opts      managerOptions
}

// NewManager attempts to connect to the given set of node addresses and if
// successful returns a new Manager containing connections to those nodes.
func NewManager(nodeAddrs []string, opts ...ManagerOption) (*Manager, error) {
	if len(nodeAddrs) == 0 {
		return nil, fmt.Errorf("could not create manager: no nodes provided")
	}

	m := &Manager{
		lookup:  make(map[uint32]*Node),
		configs: make(map[uint32]*Configuration),
	}

	for _, opt := range opts {
		opt(&m.opts)
	}

	for _, naddr := range nodeAddrs {
		node, err2 := m.createNode(naddr)
		if err2 != nil {
			return nil, ManagerCreationError(err2)
		}
		m.lookup[node.id] = node
		m.nodes = append(m.nodes, node)
	}

	if m.opts.trace {
		title := strings.Join(nodeAddrs, ",")
		m.eventLog = trace.NewEventLog("gorums.Manager", title)
	}

	err := m.connectAll()
	if err != nil {
		return nil, ManagerCreationError(err)
	}

	if m.opts.logger != nil {
		m.logger = m.opts.logger
	}

	if m.eventLog != nil {
		m.eventLog.Printf("ready")
	}

	return m, nil
}

func (m *Manager) createNode(addr string) (*Node, error) {
	m.Lock()
	defer m.Unlock()

	tcpAddr, err := net.ResolveTCPAddr("tcp", addr)
	if err != nil {
		return nil, fmt.Errorf("create node %s error: %v", addr, err)
	}

	h := fnv.New32a()
	_, _ = h.Write([]byte(tcpAddr.String()))
	id := h.Sum32()

	if _, found := m.lookup[id]; found {
		return nil, fmt.Errorf("create node %s error: node already exists", addr)
	}

	node := &Node{
		id:      id,
		addr:    tcpAddr.String(),
		latency: -1 * time.Second,
	}

	return node, nil
}

func (m *Manager) connectAll() error {
	if m.opts.noConnect {
		return nil
	}

	if m.eventLog != nil {
		m.eventLog.Printf("connecting")
	}

	for _, node := range m.nodes {
		err := node.connect(m.opts.grpcDialOpts...)
		if err != nil {
			if m.eventLog != nil {
				m.eventLog.Errorf("connect failed, error connecting to node %s, error: %v", node.addr, err)
			}
			return fmt.Errorf("connect node %s error: %v", node.addr, err)
		}
	}
	return nil
}

func (m *Manager) closeNodeConns() {
	for _, node := range m.nodes {
		err := node.close()
		if err == nil {
			continue
		}
		if m.logger != nil {
			m.logger.Printf("node %d: error closing: %v", node.id, err)
		}
	}
}

// Close closes all node connections and any client streams.
func (m *Manager) Close() {
	m.closeOnce.Do(func() {
		if m.eventLog != nil {
			m.eventLog.Printf("closing")
		}
		m.closeNodeConns()
	})
}

// NodeIDs returns the identifier of each available node. IDs are returned in
// the same order as they were provided in the creation of the Manager.
func (m *Manager) NodeIDs() []uint32 {
	m.Lock()
	defer m.Unlock()
	ids := make([]uint32, 0, len(m.nodes))
	for _, node := range m.nodes {
		ids = append(ids, node.ID())
	}
	return ids
}

// Node returns the node with the given identifier if present.
func (m *Manager) Node(id uint32) (node *Node, found bool) {
	m.Lock()
	defer m.Unlock()
	node, found = m.lookup[id]
	return node, found
}

// Nodes returns a slice of each available node. IDs are returned in the same
// order as they were provided in the creation of the Manager.
func (m *Manager) Nodes() []*Node {
	m.Lock()
	defer m.Unlock()
	return m.nodes
}

// ConfigurationIDs returns the identifier of each available
// configuration.
func (m *Manager) ConfigurationIDs() []uint32 {
	m.Lock()
	defer m.Unlock()
	ids := make([]uint32, 0, len(m.configs))
	for id := range m.configs {
		ids = append(ids, id)
	}
	return ids
}

// Configuration returns the configuration with the given global
// identifier if present.
func (m *Manager) Configuration(id uint32) (config *Configuration, found bool) {
	m.Lock()
	defer m.Unlock()
	config, found = m.configs[id]
	return config, found
}

// Configurations returns a slice of each available configuration.
func (m *Manager) Configurations() []*Configuration {
	m.Lock()
	defer m.Unlock()
	configs := make([]*Configuration, 0, len(m.configs))
	for _, conf := range m.configs {
		configs = append(configs, conf)
	}
	return configs
}

// Size returns the number of nodes and configurations in the Manager.
func (m *Manager) Size() (nodes, configs int) {
	m.Lock()
	defer m.Unlock()
	return len(m.nodes), len(m.configs)
}

// AddNode attempts to dial to the provide node address. The node is
// added to the Manager's pool of nodes if a connection was established.
func (m *Manager) AddNode(addr string) error {
	panic("not implemented")
}

// NewConfiguration returns a new configuration given quorum specification and
// a timeout.
func (m *Manager) NewConfiguration(ids []uint32, qspec QuorumSpec) (*Configuration, error) {
	m.Lock()
	defer m.Unlock()

	if len(ids) == 0 {
		return nil, IllegalConfigError("need at least one node")
	}

	var cnodes []*Node
	for _, nid := range ids {
		node, found := m.lookup[nid]
		if !found {
			return nil, NodeNotFoundError(nid)
		}
		cnodes = append(cnodes, node)
	}

	// Node ids are sorted ensure a globally consistent configuration id.
	sort.Sort(idSlice(ids))

	h := fnv.New32a()
	for _, id := range ids {
		binary.Write(h, binary.LittleEndian, id)
	}
	cid := h.Sum32()

	conf, found := m.configs[cid]
	if found {
		return conf, nil
	}

	c := &Configuration{
		id:    cid,
		nodes: cnodes,
		n:     len(cnodes),
		mgr:   m,
		qspec: qspec,
	}
	m.configs[cid] = c

	return c, nil
}

type idSlice []uint32

func (p idSlice) Len() int           { return len(p) }
func (p idSlice) Less(i, j int) bool { return p[i] < p[j] }
func (p idSlice) Swap(i, j int)      { p[i], p[j] = p[j], p[i] }

/* node_func.go */

// ID returns the ID of m.
func (n *Node) ID() uint32 {
	return n.id
}

// Address returns network address of m.
func (n *Node) Address() string {
	return n.addr
}

func (n *Node) String() string {
	n.Lock()
	defer n.Unlock()
	return fmt.Sprintf(
		"node %d | addr: %s | latency: %v",
		n.id, n.addr, n.latency,
	)
}

func (n *Node) setLastErr(err error) {
	n.Lock()
	defer n.Unlock()
	n.lastErr = err
}

// LastErr returns the last error encountered (if any) when invoking a remote
// procedure call on this node.
func (n *Node) LastErr() error {
	n.Lock()
	defer n.Unlock()
	return n.lastErr
}

func (n *Node) setLatency(lat time.Duration) {
	n.Lock()
	defer n.Unlock()
	n.latency = lat
}

// Latency returns the latency of the last successful remote procedure call
// made to this node.
func (n *Node) Latency() time.Duration {
	n.Lock()
	defer n.Unlock()
	return n.latency
}

type lessFunc func(n1, n2 *Node) bool

// MultiSorter implements the Sort interface, sorting the nodes within.
type MultiSorter struct {
	nodes []*Node
	less  []lessFunc
}

// Sort sorts the argument slice according to the less functions passed to
// OrderedBy.
func (ms *MultiSorter) Sort(nodes []*Node) {
	ms.nodes = nodes
	sort.Sort(ms)
}

// OrderedBy returns a Sorter that sorts using the less functions, in order.
// Call its Sort method to sort the data.
func OrderedBy(less ...lessFunc) *MultiSorter {
	return &MultiSorter{
		less: less,
	}
}

// Len is part of sort.Interface.
func (ms *MultiSorter) Len() int {
	return len(ms.nodes)
}

// Swap is part of sort.Interface.
func (ms *MultiSorter) Swap(i, j int) {
	ms.nodes[i], ms.nodes[j] = ms.nodes[j], ms.nodes[i]
}

// Less is part of sort.Interface. It is implemented by looping along the
// less functions until it finds a comparison that is either Less or
// !Less. Note that it can call the less functions twice per call. We
// could change the functions to return -1, 0, 1 and reduce the
// number of calls for greater efficiency: an exercise for the reader.
func (ms *MultiSorter) Less(i, j int) bool {
	p, q := ms.nodes[i], ms.nodes[j]
	// Try all but the last comparison.
	var k int
	for k = 0; k < len(ms.less)-1; k++ {
		less := ms.less[k]
		switch {
		case less(p, q):
			// p < q, so we have a decision.
			return true
		case less(q, p):
			// p > q, so we have a decision.
			return false
		}
		// p == q; try the next comparison.
	}
	// All comparisons to here said "equal", so just return whatever
	// the final comparison reports.
	return ms.less[k](p, q)
}

// ID sorts nodes by their identifier in increasing order.
var ID = func(n1, n2 *Node) bool {
	return n1.id < n2.id
}

// Latency sorts nodes by latency in increasing order. Latencies less then
// zero (sentinel value) are considered greater than any positive latency.
var Latency = func(n1, n2 *Node) bool {
	if n1.latency < 0 {
		return false
	}
	return n1.latency < n2.latency

}

// Error sorts nodes by their LastErr() status in increasing order. A
// node with LastErr() != nil is larger than a node with LastErr() == nil.
var Error = func(n1, n2 *Node) bool {
	if n1.lastErr != nil && n2.lastErr == nil {
		return false
	}
	return true
}

/* opts.go */

type managerOptions struct {
	grpcDialOpts []grpc.DialOption
	logger       *log.Logger
	noConnect    bool
	trace        bool
}

// ManagerOption provides a way to set different options on a new Manager.
type ManagerOption func(*managerOptions)

// WithGrpcDialOptions returns a ManagerOption which sets any gRPC dial options
// the Manager should use when initially connecting to each node in its
// pool.
func WithGrpcDialOptions(opts ...grpc.DialOption) ManagerOption {
	return func(o *managerOptions) {
		o.grpcDialOpts = opts
	}
}

// WithLogger returns a ManagerOption which sets an optional error logger for
// the Manager.
func WithLogger(logger *log.Logger) ManagerOption {
	return func(o *managerOptions) {
		o.logger = logger
	}
}

// WithNoConnect returns a ManagerOption which instructs the Manager not to
// connect to any of its nodes. Mainly used for testing purposes.
func WithNoConnect() ManagerOption {
	return func(o *managerOptions) {
		o.noConnect = true
	}
}

// WithTracing controls whether to trace qourum calls for this Manager instance
// using the golang.org/x/net/trace package. Tracing is currently only supported
// for regular quorum calls.
func WithTracing() ManagerOption {
	return func(o *managerOptions) {
		o.trace = true
	}
}

/* trace.go */

type traceInfo struct {
	tr        trace.Trace
	firstLine firstLine
}

type firstLine struct {
	deadline time.Duration
	cid      uint32
}

func (f *firstLine) String() string {
	var line bytes.Buffer
	io.WriteString(&line, "QC: to config")
	fmt.Fprintf(&line, "%v deadline:", f.cid)
	if f.deadline != 0 {
		fmt.Fprint(&line, f.deadline)
	} else {
		io.WriteString(&line, "none")
	}
	return line.String()
}

type payload struct {
	sent bool
	id   uint32
	msg  interface{}
}

func (p payload) String() string {
	if p.sent {
		return fmt.Sprintf("sent: %v", p.msg)
	}
	return fmt.Sprintf("recv from %d: %v", p.id, p.msg)
}

type qcresult struct {
	ids   []uint32
	reply interface{}
	err   error
}

func (q qcresult) String() string {
	var out bytes.Buffer
	io.WriteString(&out, "recv QC reply: ")
	fmt.Fprintf(&out, "ids: %v, ", q.ids)
	fmt.Fprintf(&out, "reply: %v ", q.reply)
	if q.err != nil {
		fmt.Fprintf(&out, ", error: %v", q.err)
	}
	return out.String()
}

/* util.go */

func appendIfNotPresent(set []uint32, x uint32) []uint32 {
	for _, y := range set {
		if y == x {
			return set
		}
	}
	return append(set, x)
}

// Reference imports to suppress errors if they are not otherwise used.
var _ context.Context
var _ grpc.ClientConn

// This is a compile-time assertion to ensure that this generated file
// is compatible with the grpc package it is being compiled against.
const _ = grpc.SupportPackageIsVersion4

// Client API for Raft service

type RaftClient interface {
	RequestVote(ctx context.Context, in *raftpb.RequestVoteRequest, opts ...grpc.CallOption) (*raftpb.RequestVoteResponse, error)
	AppendEntries(ctx context.Context, in *raftpb.AppendEntriesRequest, opts ...grpc.CallOption) (*raftpb.AppendEntriesResponse, error)
	ClientCommand(ctx context.Context, in *raftpb.ClientCommandRequest, opts ...grpc.CallOption) (*raftpb.ClientCommandResponse, error)
}

type raftClient struct {
	cc *grpc.ClientConn
}

func NewRaftClient(cc *grpc.ClientConn) RaftClient {
	return &raftClient{cc}
}

func (c *raftClient) RequestVote(ctx context.Context, in *raftpb.RequestVoteRequest, opts ...grpc.CallOption) (*raftpb.RequestVoteResponse, error) {
	out := new(raftpb.RequestVoteResponse)
	err := grpc.Invoke(ctx, "/gorumspb.Raft/RequestVote", in, out, c.cc, opts...)
	if err != nil {
		return nil, err
	}
	return out, nil
}

func (c *raftClient) AppendEntries(ctx context.Context, in *raftpb.AppendEntriesRequest, opts ...grpc.CallOption) (*raftpb.AppendEntriesResponse, error) {
	out := new(raftpb.AppendEntriesResponse)
	err := grpc.Invoke(ctx, "/gorumspb.Raft/AppendEntries", in, out, c.cc, opts...)
	if err != nil {
		return nil, err
	}
	return out, nil
}

func (c *raftClient) ClientCommand(ctx context.Context, in *raftpb.ClientCommandRequest, opts ...grpc.CallOption) (*raftpb.ClientCommandResponse, error) {
	out := new(raftpb.ClientCommandResponse)
	err := grpc.Invoke(ctx, "/gorumspb.Raft/ClientCommand", in, out, c.cc, opts...)
	if err != nil {
		return nil, err
	}
	return out, nil
}

// Server API for Raft service

type RaftServer interface {
	RequestVote(context.Context, *raftpb.RequestVoteRequest) (*raftpb.RequestVoteResponse, error)
	AppendEntries(context.Context, *raftpb.AppendEntriesRequest) (*raftpb.AppendEntriesResponse, error)
	ClientCommand(context.Context, *raftpb.ClientCommandRequest) (*raftpb.ClientCommandResponse, error)
}

func RegisterRaftServer(s *grpc.Server, srv RaftServer) {
	s.RegisterService(&_Raft_serviceDesc, srv)
}

func _Raft_RequestVote_Handler(srv interface{}, ctx context.Context, dec func(interface{}) error, interceptor grpc.UnaryServerInterceptor) (interface{}, error) {
	in := new(raftpb.RequestVoteRequest)
	if err := dec(in); err != nil {
		return nil, err
	}
	if interceptor == nil {
		return srv.(RaftServer).RequestVote(ctx, in)
	}
	info := &grpc.UnaryServerInfo{
		Server:     srv,
		FullMethod: "/gorumspb.Raft/RequestVote",
	}
	handler := func(ctx context.Context, req interface{}) (interface{}, error) {
		return srv.(RaftServer).RequestVote(ctx, req.(*raftpb.RequestVoteRequest))
	}
	return interceptor(ctx, in, info, handler)
}

func _Raft_AppendEntries_Handler(srv interface{}, ctx context.Context, dec func(interface{}) error, interceptor grpc.UnaryServerInterceptor) (interface{}, error) {
	in := new(raftpb.AppendEntriesRequest)
	if err := dec(in); err != nil {
		return nil, err
	}
	if interceptor == nil {
		return srv.(RaftServer).AppendEntries(ctx, in)
	}
	info := &grpc.UnaryServerInfo{
		Server:     srv,
		FullMethod: "/gorumspb.Raft/AppendEntries",
	}
	handler := func(ctx context.Context, req interface{}) (interface{}, error) {
		return srv.(RaftServer).AppendEntries(ctx, req.(*raftpb.AppendEntriesRequest))
	}
	return interceptor(ctx, in, info, handler)
}

func _Raft_ClientCommand_Handler(srv interface{}, ctx context.Context, dec func(interface{}) error, interceptor grpc.UnaryServerInterceptor) (interface{}, error) {
	in := new(raftpb.ClientCommandRequest)
	if err := dec(in); err != nil {
		return nil, err
	}
	if interceptor == nil {
		return srv.(RaftServer).ClientCommand(ctx, in)
	}
	info := &grpc.UnaryServerInfo{
		Server:     srv,
		FullMethod: "/gorumspb.Raft/ClientCommand",
	}
	handler := func(ctx context.Context, req interface{}) (interface{}, error) {
		return srv.(RaftServer).ClientCommand(ctx, req.(*raftpb.ClientCommandRequest))
	}
	return interceptor(ctx, in, info, handler)
}

var _Raft_serviceDesc = grpc.ServiceDesc{
	ServiceName: "gorumspb.Raft",
	HandlerType: (*RaftServer)(nil),
	Methods: []grpc.MethodDesc{
		{
			MethodName: "RequestVote",
			Handler:    _Raft_RequestVote_Handler,
		},
		{
			MethodName: "AppendEntries",
			Handler:    _Raft_AppendEntries_Handler,
		},
		{
			MethodName: "ClientCommand",
			Handler:    _Raft_ClientCommand_Handler,
		},
	},
	Streams:  []grpc.StreamDesc{},
	Metadata: "pkg/raft/gorumspb/gorums.proto",
}

func init() { proto.RegisterFile("pkg/raft/gorumspb/gorums.proto", fileDescriptorGorums) }

var fileDescriptorGorums = []byte{
	// 259 bytes of a gzipped FileDescriptorProto
	0x1f, 0x8b, 0x08, 0x00, 0x00, 0x09, 0x6e, 0x88, 0x02, 0xff, 0xe2, 0x92, 0x2b, 0xc8, 0x4e, 0xd7,
	0x2f, 0x4a, 0x4c, 0x2b, 0xd1, 0x4f, 0xcf, 0x2f, 0x2a, 0xcd, 0x2d, 0x2e, 0x48, 0x82, 0x32, 0xf4,
	0x0a, 0x8a, 0xf2, 0x4b, 0xf2, 0x85, 0x38, 0x60, 0xc2, 0x52, 0x2a, 0xe9, 0x99, 0x25, 0x19, 0xa5,
	0x49, 0x7a, 0xc9, 0xf9, 0xb9, 0xfa, 0x45, 0xa9, 0x39, 0x89, 0x30, 0x85, 0x28, 0xea, 0xa5, 0x0c,
	0x30, 0x54, 0x81, 0x0d, 0x87, 0xdb, 0x02, 0x22, 0x0a, 0x20, 0x82, 0x10, 0x1d, 0x46, 0xff, 0x18,
	0xb9, 0x58, 0x82, 0x12, 0xd3, 0x4a, 0x84, 0x02, 0xb8, 0xb8, 0x83, 0x52, 0x0b, 0x4b, 0x53, 0x8b,
	0x4b, 0xc2, 0xf2, 0x4b, 0x52, 0x85, 0xa4, 0xf4, 0x20, 0x6a, 0xf5, 0x90, 0x04, 0xa1, 0x4c, 0x29,
	0x69, 0xac, 0x72, 0xc5, 0x05, 0xf9, 0x79, 0xc5, 0xa9, 0x4a, 0x1c, 0x0b, 0xb6, 0x4a, 0x30, 0xae,
	0xd8, 0x2a, 0xc1, 0x28, 0x14, 0xc6, 0xc5, 0xeb, 0x58, 0x50, 0x90, 0x9a, 0x97, 0xe2, 0x9a, 0x57,
	0x52, 0x94, 0x99, 0x5a, 0x2c, 0x24, 0x03, 0xd3, 0x87, 0x22, 0x0c, 0x33, 0x55, 0x16, 0x87, 0x2c,
	0xcc, 0xdc, 0x06, 0x98, 0xb9, 0x3e, 0x5c, 0xbc, 0xce, 0x39, 0x99, 0xa9, 0x79, 0x25, 0xce, 0xf9,
	0xb9, 0xb9, 0x89, 0x79, 0x29, 0x08, 0x73, 0x51, 0x84, 0x31, 0xcc, 0x45, 0x93, 0x85, 0x98, 0xeb,
	0xa4, 0x73, 0xe1, 0xa1, 0x1c, 0xc3, 0x8d, 0x87, 0x72, 0x0c, 0x0f, 0x1e, 0xca, 0x31, 0x36, 0x3c,
	0x92, 0x63, 0x5c, 0xf1, 0x48, 0x8e, 0xf1, 0xc4, 0x23, 0x39, 0xc6, 0x0b, 0x8f, 0xe4, 0x18, 0x1f,
	0x3c, 0x92, 0x63, 0x7c, 0xf1, 0x48, 0x8e, 0xe1, 0xc3, 0x23, 0x39, 0xc6, 0x09, 0x8f, 0xe5, 0x18,
	0x92, 0xd8, 0xc0, 0xa1, 0x66, 0x0c, 0x08, 0x00, 0x00, 0xff, 0xff, 0x0c, 0x93, 0x64, 0x35, 0xb9,
	0x01, 0x00, 0x00,
}
