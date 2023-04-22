package DIY_RPC

import (
	"fmt"
	"reflect"
	"testing"
)

/* 新增服务Wzz.Hello */
type Wzz int

type Args struct {
	Num1, Num2 int
}

func (w *Wzz) Hello(args Args, reply *string) error {
	*reply = fmt.Sprintf("Hello from wzz: Num1[%d], Num2[%d]\n", args.Num1, args.Num2)
	return nil
}

// 非导出方法, 不应该被注册到服务的方法集合中
func (w *Wzz) hello(args Args, reply *string) error {
	*reply = fmt.Sprintf("Hello from wzz: Num1[%d], Num2[%d]\n", args.Num1, args.Num2)
	return nil
}

func _assert(condition bool, msg string, v ...interface{}) {
	if !condition {
		panic(fmt.Sprintf("assertion failed: "+msg, v...))
	}
}

// 测试newService方法
func TestNewService(t *testing.T) {
	var wzz Wzz
	// 创建一个服务
	service := newService(&wzz)

	// 测试服务的名称、注册的导出方法数量、导出方法中是否包含Hello
	_assert(service.name == "Wzz", "service's name is wrong")
	_assert(len(service.methodMap) == 1, "wrong service method, expect 1, but got %d", len(service.methodMap))

	mType := service.methodMap["Hello"]
	_assert(mType != nil, "wrong Method: Hello shouldn't nil")

}

// 测试call方法
func TestCall(t *testing.T) {
	rsvr := Wzz(0)
	service := newService(&rsvr)

	// 调用Wzz.Hello方法
	targetMethodTyp := service.methodMap["Hello"]

	var replyVal string
	_ = service.call(targetMethodTyp, reflect.ValueOf(Args{
		Num1: 1,
		Num2: 2,
	}), reflect.ValueOf(&replyVal))

	fmt.Println("result:", replyVal)
	// result: Hello from wzz: Num1[1], Num2[2]
}
