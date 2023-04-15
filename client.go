package DIY_RPC


type Call struct{
	Seq 			uint64
	ServiceMethod 	string 			// 格式: "<service>.<method>"
	Args 			interface{}		// rpc的参数
	Reply 			interface{}		// rpc调用的返回值
	Error			error			// 错误出现时设置
	Done			chan *Call		// 异步调用: 调用结束时, 调用call.done()通知调用发起方
}