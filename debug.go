package DIY_RPC

import (
	"fmt"
	"net/http"
	"reflect"
	"text/template"
)

// 模板字符串中, 遍历多个debugService实例, 打印这些实例中注册的rpc方法集合
const debugTemp = `<html>
<body>
<title>DIY_RPC Services</title>
{{range .}}
<hr>
	<table>
		<tr align=center>
			<th>Method</th>
			<th>Calls</th>
		</tr>
	{{range $name, $mType := .Method}}
		<tr>
			<td align=left>{{$name}}({{$mType.ArgType}}, {{$mType.ReplyType}}) {{$mType | getRet}}</td>
			<td align=center>{{$mType.NumCalls}}</td>
		</tr>
	{{end}}
	</table>
{{end}}
</body>
</html>
`

func getRet(mType *methodType) reflect.Type {
	if mType == nil {
		return nil
	}
	mthType := mType.method.Type
	if mthType.NumOut() != 1 {
		return nil
	}
	return mthType.Out(0)
}

type debugHTTP struct {
	*Server
}

type debugService struct {
	Name   string
	Method map[string]*methodType
}

// 处理/debug/DIY_RPC请求
func (server debugHTTP) ServeHTTP(w http.ResponseWriter, req *http.Request) {
	var services []debugService
	// Range 遍历sync.Map中的所有k-v, 如果返回false, 停止迭代
	// 遍历到的服务, 构造debugHTTP, 加入到services切片中
	server.serviceMap.Range(func(nameInf, svcInf interface{}) bool {
		svc := svcInf.(*service)
		services = append(services, debugService{
			Name:   svc.name,
			Method: svc.methodMap,
		})
		return true
	})
	// Parse解析模板, 如果报错, 则panic
	var debugTmp = template.Must(template.New("RPC debug").Funcs(template.FuncMap{
		"getRet": getRet,
	}).Parse(debugTemp))

	// 执行debug模板, 第一个参数为输出流io.Writer; 第二个参数为数据源, 这里选择services切片
	// 这里返回一个HTML报文, 用于展示注册在Server上的每一个服务提供的每一个方法, 及其调用情况
	err := debugTmp.Execute(w, services)
	if err != nil {
		_, _ = fmt.Fprintln(w, "rpc: error executing template:", err.Error())
	}
}
