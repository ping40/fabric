/*
Copyright IBM Corp. All Rights Reserved.

SPDX-License-Identifier: Apache-2.0
*/

package algo

import (
	"sync"
	"sync/atomic"
	"time"

	"github.com/hyperledger/fabric/gossip/util"
	"github.com/spf13/viper"
)

/* PullEngine is an object that performs pull-based gossip, and maintains an internal state of items
   identified by string numbers.
   The protocol is as follows:
   1) The Initiator sends a Hello message with a specific NONCE to a set of remote peers.
   2) Each remote peer responds with a digest of its messages and returns that NONCE.
   3) The initiator checks the validity of the NONCEs received, aggregates the digests,
      and crafts a request containing specific item ids it wants to receive from each remote peer and then
      sends each request to its corresponding peer.
   4) Each peer sends back the response containing the items requested, if it still holds them and the NONCE.

    Other peer				   			   Initiator
	 O	<-------- Hello <NONCE> -------------------------	O
	/|\	--------- Digest <[3,5,8, 10...], NONCE> -------->     /|\
	 |	<-------- Request <[3,8], NONCE> -----------------      |   过滤工作由 pullstore.go IngressDigFilter 完成
	/ \	--------- Response <[item3, item8], NONCE>------->     / \

engine引擎执行的过程中有两个关键词，一个是摘要，一个是NONCE。摘要指从消息中择取出的足以代表
一条消息的关键信息，身份消息的摘要即为PKI-ID，块消息的摘要则为块序号，在接收到摘要应答后，
调用OnRes()将摘要最终存储到engine成员state中，这些摘要来自于Mediator模块处理消息的函数
HandleMessage中处理摘要应答的分支中itemIDs[i] = p.IdExtractor(msg)，即使用的是Mediator
模块的适配器中封装的IdExtractor函数，也因此，由于certStore模块和chanState模块所实现的适配器中的
IdExtractor不一样，所有才能从消息中分离出两种不同的摘要。NONCE是通信安全中的一个概念，指的是用一次
即废弃的一个整数，目的在于防止replay attack，但是在这里只是简单使用NONCE一次性的性质，在engine
引擎周期性执行pull的过程中，每个周期中的步骤之间不产生交叉混淆。

*/

const (
	defDigestWaitTime   = time.Duration(1000) * time.Millisecond
	defRequestWaitTime  = time.Duration(1500) * time.Millisecond
	defResponseWaitTime = time.Duration(2000) * time.Millisecond
)

// SetDigestWaitTime sets the digest wait time
func SetDigestWaitTime(time time.Duration) {
	viper.Set("peer.gossip.digestWaitTime", time)
}

// SetRequestWaitTime sets the request wait time
func SetRequestWaitTime(time time.Duration) {
	viper.Set("peer.gossip.requestWaitTime", time)
}

// SetResponseWaitTime sets the response wait time
func SetResponseWaitTime(time time.Duration) {
	viper.Set("peer.gossip.responseWaitTime", time)
}

// DigestFilter filters digests to be sent to a remote peer that
// sent a hello or a request, based on its messages's context
type DigestFilter func(context interface{}) func(digestItem string) bool

// PullAdapter is needed by the PullEngine in order to
// send messages to the remote PullEngine instances.
// The PullEngine expects to be invoked with
// OnHello, OnDigest, OnReq, OnRes when the respective message arrives
// from a remote PullEngine
type PullAdapter interface {
	// SelectPeers returns a slice of peers which the engine will initiate the protocol with
	SelectPeers() []string

	// Hello sends a hello message to initiate the protocol
	// and returns an NONCE that is expected to be returned
	// in the digest message.
	Hello(dest string, nonce uint64)

	// SendDigest sends a digest to a remote PullEngine.
	// The context parameter specifies the remote engine to send to.
	SendDigest(digest []string, nonce uint64, context interface{})

	// SendReq sends an array of items to a certain remote PullEngine identified
	// by a string
	SendReq(dest string, items []string, nonce uint64)

	// SendRes sends an array of items to a remote PullEngine identified by a context.
	SendRes(items []string, context interface{}, nonce uint64)
}

// PullEngine is the component that actually invokes the pull algorithm
// with the help of the PullAdapter
type PullEngine struct {
	PullAdapter
	stopFlag           int32
	state              *util.Set
	item2owners        map[string][]string
	peers2nonces       map[string]uint64
	nonces2peers       map[uint64]string
	acceptingDigests   int32
	acceptingResponses int32
	lock               sync.Mutex
	outgoingNONCES     *util.Set
	incomingNONCES     *util.Set
	digFilter          DigestFilter

	digestWaitTime   time.Duration
	requestWaitTime  time.Duration
	responseWaitTime time.Duration
}

// NewPullEngineWithFilter creates an instance of a PullEngine with a certain sleep time
// between pull initiations, and uses the given filters when sending digests and responses
func NewPullEngineWithFilter(participant PullAdapter, sleepTime time.Duration, df DigestFilter) *PullEngine {
	engine := &PullEngine{
		PullAdapter:        participant, //（虽然是成员，但是我觉得一个对象的适配器和这个对象应该是平级的关系）
		stopFlag:           int32(0),
		state:              util.NewSet(),
		item2owners:        make(map[string][]string),
		peers2nonces:       make(map[string]uint64),
		nonces2peers:       make(map[uint64]string),
		acceptingDigests:   int32(0),
		acceptingResponses: int32(0),
		incomingNONCES:     util.NewSet(),
		outgoingNONCES:     util.NewSet(),
		digFilter:          df,
		digestWaitTime:     util.GetDurationOrDefault("peer.gossip.digestWaitTime", defDigestWaitTime),
		requestWaitTime:    util.GetDurationOrDefault("peer.gossip.requestWaitTime", defRequestWaitTime),
		responseWaitTime:   util.GetDurationOrDefault("peer.gossip.responseWaitTime", defResponseWaitTime),
	}

	go func() {
		for !engine.toDie() {
			time.Sleep(sleepTime)
			if engine.toDie() {
				return
			}
			engine.initiatePull()
		}
	}()

	return engine
}

// NewPullEngine creates an instance of a PullEngine with a certain sleep time
// between pull initiations
func NewPullEngine(participant PullAdapter, sleepTime time.Duration) *PullEngine {
	acceptAllFilter := func(_ interface{}) func(string) bool {
		return func(_ string) bool {
			return true
		}
	}
	return NewPullEngineWithFilter(participant, sleepTime, acceptAllFilter)
}

func (engine *PullEngine) toDie() bool {
	return atomic.LoadInt32(&(engine.stopFlag)) == int32(1)
}

func (engine *PullEngine) acceptResponses() {
	atomic.StoreInt32(&(engine.acceptingResponses), int32(1))
}

func (engine *PullEngine) isAcceptingResponses() bool {
	return atomic.LoadInt32(&(engine.acceptingResponses)) == int32(1)
}

func (engine *PullEngine) acceptDigests() {
	atomic.StoreInt32(&(engine.acceptingDigests), int32(1))
}

func (engine *PullEngine) isAcceptingDigests() bool {
	return atomic.LoadInt32(&(engine.acceptingDigests)) == int32(1)
}

func (engine *PullEngine) ignoreDigests() {
	atomic.StoreInt32(&(engine.acceptingDigests), int32(0))
}

// Stop stops the engine
func (engine *PullEngine) Stop() {
	atomic.StoreInt32(&(engine.stopFlag), int32(1))
}

func (engine *PullEngine) initiatePull() {
	engine.lock.Lock()
	defer engine.lock.Unlock()

	engine.acceptDigests()
	for _, peer := range engine.SelectPeers() {
		nonce := engine.newNONCE()
		engine.outgoingNONCES.Add(nonce)
		engine.nonces2peers[nonce] = peer
		engine.peers2nonces[peer] = nonce
		engine.Hello(peer, nonce)
	}

	time.AfterFunc(engine.digestWaitTime, func() {
		engine.processIncomingDigests()
	})
}

func (engine *PullEngine) processIncomingDigests() {
	engine.ignoreDigests()

	engine.lock.Lock()
	defer engine.lock.Unlock()

	requestMapping := make(map[string][]string)
	for n, sources := range engine.item2owners {
		// select a random source
		source := sources[util.RandomInt(len(sources))] // 表明同一个消息，多个peer拥有
		if _, exists := requestMapping[source]; !exists {
			requestMapping[source] = make([]string, 0)
		}
		// append the number to that source
		requestMapping[source] = append(requestMapping[source], n)
	}

	engine.acceptResponses()

	for dest, seqsToReq := range requestMapping {
		engine.SendReq(dest, seqsToReq, engine.peers2nonces[dest])
	}

	time.AfterFunc(engine.responseWaitTime, engine.endPull)
}

func (engine *PullEngine) endPull() {
	engine.lock.Lock()
	defer engine.lock.Unlock()

	atomic.StoreInt32(&(engine.acceptingResponses), int32(0))
	engine.outgoingNONCES.Clear()

	engine.item2owners = make(map[string][]string)
	engine.peers2nonces = make(map[string]uint64)
	engine.nonces2peers = make(map[uint64]string)
}

// OnDigest notifies the engine that a digest has arrived
func (engine *PullEngine) OnDigest(digest []string, nonce uint64, context interface{}) {
	if !engine.isAcceptingDigests() || !engine.outgoingNONCES.Exists(nonce) {
		return
	}

	engine.lock.Lock()
	defer engine.lock.Unlock()

	for _, n := range digest {
		if engine.state.Exists(n) {
			continue
		}

		if _, exists := engine.item2owners[n]; !exists {
			engine.item2owners[n] = make([]string, 0)
		}

		engine.item2owners[n] = append(engine.item2owners[n], engine.nonces2peers[nonce])
	}
}

// Add adds items to the state
func (engine *PullEngine) Add(seqs ...string) {
	for _, seq := range seqs {
		engine.state.Add(seq)
	}
}

// Remove removes items from the state
func (engine *PullEngine) Remove(seqs ...string) {
	for _, seq := range seqs {
		engine.state.Remove(seq)
	}
}

// OnHello notifies the engine a hello has arrived
func (engine *PullEngine) OnHello(nonce uint64, context interface{}) {
	engine.incomingNONCES.Add(nonce)

	time.AfterFunc(engine.requestWaitTime, func() {
		engine.incomingNONCES.Remove(nonce)
	})

	a := engine.state.ToArray()
	var digest []string
	filter := engine.digFilter(context)
	for _, item := range a {
		dig := item.(string)
		if !filter(dig) {
			continue
		}
		digest = append(digest, dig)
	}
	if len(digest) == 0 {
		return
	}
	engine.SendDigest(digest, nonce, context)
}

// OnReq notifies the engine a request has arrived
func (engine *PullEngine) OnReq(items []string, nonce uint64, context interface{}) {
	if !engine.incomingNONCES.Exists(nonce) {
		return
	}
	engine.lock.Lock()
	defer engine.lock.Unlock()

	filter := engine.digFilter(context)
	var items2Send []string
	for _, item := range items {
		if engine.state.Exists(item) && filter(item) {
			items2Send = append(items2Send, item)
		}
	}

	if len(items2Send) == 0 {
		return
	}

	go engine.SendRes(items2Send, context, nonce)
}

// OnRes notifies the engine a response has arrived
func (engine *PullEngine) OnRes(items []string, nonce uint64) {
	if !engine.outgoingNONCES.Exists(nonce) || !engine.isAcceptingResponses() {
		return
	}

	engine.Add(items...)
}

func (engine *PullEngine) newNONCE() uint64 {
	n := uint64(0)
	for {
		n = util.RandomUInt64()
		if !engine.outgoingNONCES.Exists(n) {
			return n
		}
	}
}
