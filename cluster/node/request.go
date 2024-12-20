/**
 * @Author: fuxiao
 * @Email: 576101059@qq.com
 * @Date: 2022/6/19 12:20 下午
 * @Desc: TODO
 */

package node

import (
	"context"
	"github.com/dobyte/due/v2/cluster"
	"github.com/dobyte/due/v2/core/chains"
	"github.com/dobyte/due/v2/errors"
	"github.com/dobyte/due/v2/session"
	"github.com/dobyte/due/v2/task"
	"github.com/jinzhu/copier"
	"sync/atomic"
)

type request struct {
	node    *Node
	ctx     context.Context  // 上下文
	gid     string           // 来源网关ID
	nid     string           // 来源节点ID
	pid     string           // 来源Actor ID
	cid     int64            // 连接ID
	uid     int64            // 用户ID
	message *cluster.Message // 请求消息
	version atomic.Int32     // 对象版本号
	chain   *chains.Chain    // 调用链
}

// GID 获取网关ID
func (r *request) GID() string {
	return r.gid
}

// NID 获取节点ID
func (r *request) NID() string {
	return r.nid
}

// CID 获取连接ID
func (r *request) CID() int64 {
	return r.cid
}

// UID 获取用户ID
func (r *request) UID() int64 {
	return r.uid
}

// Seq 获取消息序列号
func (r *request) Seq() int32 {
	return r.message.Seq
}

// Route 获取消息路由号
func (r *request) Route() int32 {
	return r.message.Route
}

// Event 获取事件类型
func (r *request) Event() cluster.Event {
	return 0
}

// Kind 上下文消息类型
func (r *request) Kind() Kind {
	return Request
}

// Parse 解析消息
func (r *request) Parse(v interface{}) error {
	msg, ok := r.message.Data.([]byte)
	if !ok {
		return copier.CopyWithOption(v, r.message.Data, copier.Option{
			DeepCopy: true,
		})
	}

	if len(msg) == 0 {
		return nil
	}

	if r.gid != "" && r.node.opts.encryptor != nil {
		data, err := r.node.opts.encryptor.Decrypt(msg)
		if err != nil {
			return err
		}

		return r.node.opts.codec.Unmarshal(data, v)
	}

	return r.node.opts.codec.Unmarshal(msg, v)
}

// Defer 添加defer延迟调用栈
// 此方法功能与go defer一致，作用域也仅限于当前handler处理函数内，推荐使用Defer方法替代go defer使用
// 区别在于使用Defer方法可以对调用栈进行取消操作
// 同时，在调用Task和Next方法是会自动取消调用栈
// 也可通过Cancel方法进行手动取消
// bottom用于标识是否挂载到栈底部
func (r *request) Defer(fn func(), bottom ...bool) {
	if r.chain == nil {
		r.chain = chains.NewChain()
	}

	if len(bottom) > 0 && bottom[0] {
		r.chain.AddToTail(fn)
	} else {
		r.chain.AddToHead(fn)
	}
}

// Cancel 取消Defer调用栈
func (r *request) Cancel() {
	if r.chain != nil {
		r.chain.Cancel()
	}
}

// 执行defer调用栈
func (r *request) compareVersionExecDefer(version int32) {
	if r.chain != nil && r.version.Load() == version {
		r.chain.FireHead()
	}
}

// Clone 克隆Context
func (r *request) Clone() Context {
	return &request{
		node: r.node,
		gid:  r.gid,
		nid:  r.nid,
		cid:  r.cid,
		uid:  r.uid,
		ctx:  context.Background(),
		message: &cluster.Message{
			Seq:   r.message.Seq,
			Route: r.message.Route,
			Data:  r.message.Data,
		},
	}
}

// Task 投递任务
// 推荐使用此方法替代task.AddTask和go func
// 调用此方法会自动取消Defer调用栈的所有执行函数
func (r *request) Task(fn func(ctx Context)) {
	version := r.incrVersion()

	r.Cancel()

	task.AddTask(func() {
		defer r.compareVersionRecycle(version)

		defer r.compareVersionExecDefer(version)

		fn(r)
	})
}

// Next 消息下放
// 调用此方法会自动取消Defer调用栈的所有执行函数
func (r *request) Next() error {
	return r.node.scheduler.dispatch(r)
}

// Proxy 获取代理API
func (r *request) Proxy() *Proxy {
	return r.node.proxy
}

// Context 获取上下文
func (r *request) Context() context.Context {
	return r.ctx
}

// BindGate 绑定网关
func (r *request) BindGate(uid ...int64) error {
	switch {
	case len(uid) > 0:
		return r.node.proxy.BindGate(r.ctx, r.gid, r.cid, uid[0])
	case r.uid != 0:
		return r.node.proxy.BindGate(r.ctx, r.gid, r.cid, r.uid)
	default:
		return errors.ErrIllegalOperation
	}
}

// UnbindGate 解绑网关
func (r *request) UnbindGate(uid ...int64) error {
	switch {
	case len(uid) > 0:
		return r.node.proxy.UnbindGate(r.ctx, uid[0])
	case r.uid != 0:
		return r.node.proxy.UnbindGate(r.ctx, r.uid)
	default:
		return errors.ErrIllegalOperation
	}
}

// BindNode 绑定节点
func (r *request) BindNode(uid ...int64) error {
	switch {
	case len(uid) > 0:
		return r.node.proxy.BindNode(r.ctx, uid[0])
	case r.uid != 0:
		return r.node.proxy.BindNode(r.ctx, r.uid)
	default:
		return errors.ErrIllegalOperation
	}
}

// UnbindNode 解绑节点
func (r *request) UnbindNode(uid ...int64) error {
	switch {
	case len(uid) > 0:
		return r.node.proxy.UnbindNode(r.ctx, uid[0])
	case r.uid != 0:
		return r.node.proxy.UnbindNode(r.ctx, r.uid)
	default:
		return errors.ErrIllegalOperation
	}
}

// BindActor 绑定Actor
func (r *request) BindActor(kind, id string) error {
	return r.node.scheduler.bindActor(r.uid, kind, id)
}

// UnbindActor 解绑Actor
func (r *request) UnbindActor(kind string) {
	r.node.scheduler.unbindActor(r.uid, kind)
}

// Spawn 衍生出一个新的Actor
func (r *request) Spawn(creator Creator, opts ...ActorOption) (*Actor, error) {
	return r.node.scheduler.spawn(creator, opts...)
}

// Kill 杀死存在的一个Actor
func (r *request) Kill(kind, id string) bool {
	return r.node.scheduler.kill(kind, id)
}

// Actor 获取Actor
func (r *request) Actor(kind, id string) (*Actor, bool) {
	return r.node.scheduler.load(kind, id)
}

// GetIP 获取客户端IP
func (r *request) GetIP() (string, error) {
	if r.gid == "" {
		return "", errors.ErrIllegalOperation
	}

	return r.node.proxy.GetIP(r.ctx, &cluster.GetIPArgs{
		GID:    r.gid,
		Kind:   session.Conn,
		Target: r.cid,
	})
}

// Deliver 投递消息给节点处理
func (r *request) Deliver(args *cluster.DeliverArgs) error {
	return r.node.proxy.Deliver(r.ctx, args)
}

// Reply 回复消息
func (r *request) Reply(message *cluster.Message) error {
	switch {
	case r.gid != "": // 来源于网关
		return r.node.proxy.Push(r.ctx, &cluster.PushArgs{
			GID:     r.gid,
			Kind:    session.Conn,
			Target:  r.cid,
			Message: message,
		})
	case r.pid != "": // 来源于Actor
		if actor, ok := r.node.scheduler.doLoad(r.pid); ok {
			return actor.Deliver(r.uid, message)
		}

		return nil
	case r.nid != "": // 来源于其他Node
		if r.nid == r.node.opts.id {
			return nil
		}

		return r.node.proxy.Deliver(r.ctx, &cluster.DeliverArgs{
			NID:     r.nid,
			UID:     r.uid,
			Message: message,
		})
	default:
		return errors.ErrIllegalOperation
	}
}

// Response 响应消息
func (r *request) Response(message interface{}) error {
	return r.Reply(&cluster.Message{
		Route: r.message.Route,
		Seq:   r.message.Seq,
		Data:  message,
	})
}

// Disconnect 关闭来自网关的连接
func (r *request) Disconnect(force ...bool) error {
	if r.gid == "" {
		return errors.ErrIllegalOperation
	}

	return r.node.proxy.Disconnect(r.ctx, &cluster.DisconnectArgs{
		GID:    r.gid,
		Kind:   session.Conn,
		Target: r.cid,
		Force:  len(force) > 0 && force[0],
	})
}

// 增长版本号
func (r *request) incrVersion() int32 {
	return r.version.Add(1)
}

// 获取版本号
func (r *request) loadVersion() int32 {
	return r.version.Load()
}

// 比对版本号后进行回收对象
func (r *request) compareVersionRecycle(version int32) {
	if r.version.CompareAndSwap(version, 0) {
		r.node.reqPool.Put(r)
	}
}
