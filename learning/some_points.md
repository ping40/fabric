- 关于 txid的规则

    // An unique identifier that is used end-to-end.
	//  -  set by higher layers such as end user or SDK
	//  -  passed to the endorser (which will check for uniqueness)
	//  -  as the header is passed along unchanged, it will be
	//     be retrieved by the committer (uniqueness check here as well)
	//  -  to be stored in the ledger

```go
func computeTxnID(nonce, creator []byte, h hash.Hash) (string, error) {
	b := append(nonce, creator...)

	_, err := h.Write(b)
	if err != nil {
		return "", err
	}
	digest := h.Sum(nil)
	id := hex.EncodeToString(digest)

	return id, nil
}
```


	
- nonce的生成规则

```go
// GetRandomNonce returns a random byte array of length NonceSize
func GetRandomNonce() ([]byte, error) {
	return GetRandomBytes(NonceSize)
}
```

- ledgersData/chains/index 

是可以删除的，系统能够重建。?

- The ledger file doesn’t have a version indicator

目前是这样？

- ChannelHeader.Version 字段的含义

```go
3.2 Protocol Version
The current protocol version is in ChannelHeader.Version.
	This design will not use this field; however, 
	we will not remove the field just yet. 
		We can deprecate if we don’t actually need it.

Some SDK sets ChannelHeader.Version to 1, 
	but neither the Peer nor Orderer checks 
	this version field, so the SDK may ignore 
	this field in v1.1.
```

- 机器崩溃情况

```go

1: index 重建工作

This will ensure block indexes are correct, 
for example if peer had crashed before indexes got updated.
	
2： blockfile 崩溃的情况


```

- ledger记录过程

```go
1： 先提交区块到区块文件系统
2： 再记录区块检查点信息
3： 最后是索引检查点信息

结果是：区块文件系统是最完整的

scanForLastCompleteBlock 这个方法可以区别一个block是否完整写入的
每一个块都是： len +  serializeBlock(block) 格式保存的

唯一一个不用节点签名而且不通过p2p传播的消息
是账本数据区块，ta是通过orderer签名的

```

- msp

```go

1：我们强调一下，fabric不支持证书中包含RSA keys。
2：PeerLedger不同于OrdererLedger之处在于，peer结点维护一个位掩码
  （bitmask）并以此分辨有效的交易和无效的交易
  （更详细的说明请参考XX部分）。

```

- Deliver

```go
什么时候发行？
是长时间保持 ？

```

orderer 是否帕判断envelope满足policy？

- 问题

1： Gossip模块负责连接orderer和peer， 可以处理拜占庭问题

2： Leader election ---> that non-Byzantine behavior is expected.

-  //-------- ChaincodeData is stored on the LSCC -------
 

Alice, Bob, Charlie, Dave, Eve, Frank, George

- commit  如何验证  endorsement的有效性 ？

create 有 validate 进行检查

- 提交 SignedProposal 的时候如何证明是合法人员 提交的 ？

- bccsp idemix 二个选择
 
   Type for the local MSP - by default it's of type bccsp
   localMspType: bccsp
   
   
++++++++++++++++++++++++++++++++++++++++
方法论：

了解一个服务模块，基本上问题还是集中在两点：

    服务模块自身能做什么。
    系统如何使用这个模块。这里的如何包含了何时使用和如何使用两个意思。

而相较于获取身份信息这种比较简单的实现，我们更想知道身份中
的数据是如何形成的和这些数据可以做什么。

Identity是系统成员身份的代表，而其所包含的
x509证书就可以在系统中代表着这个身份，由MSP“拥有”和管理的。
HMAC - 密匙相关的哈希运算消息认证码。-->有密码的hash方法
BCCSP，是blockchain cryptographic service provider的缩写，个人译作区域链加密服务提供者

对应两种bccsp实现，这里也有两种bccsp工厂：pkcs11factory.go和swfactory.go。fabric中某一模块
一旦涉及工厂factory，则说明该模块基本就是由工厂提供“窗口函数”，供其他模块调用。这里以swfactory
为例进行讲解。

fabric gossip是基于pull的gossip。

3.7.1 msgstore模块 继续...