# Prometheus

## 1. 简述

该项目是在官方主分支`9b8cc66`基础上进行二次定制开发的，主要为适应我们使用K8S部署时的一些问题解决，并添加了一些定制化功能。

## 2. 安装

### 2.1 预编译版本

对于发布版本预编译的二进制是可用的，下载方式如下，可参考[releases](https://github.com/jwping/prometheus/releases)

```shell
wget https://github.com/jwping/prometheus/releases/download/v0.3.0/prometheus
curl -o prometheus https://github.com/jwping/prometheus/releases/download/v0.3.0/prometheus
```

> promtool工具未作改动，所以预编译版本仅提供了prometheus二进制.



## 3. 源码编译构建

要自己从源代码构建Prometheus，您需要安装一个可运行的Go环境([安装1.13或更高版本](https://golang.org/doc/install))，另外还需要[Node.js](https://nodejs.org/)和[Yarn](https://yarnpkg.com/)才能构建前端资产。

`yarn`可以在安装完`node.js`后使用`npm install -g yarn`直接安装，`-g`表示全局安装，会将其放在`/usr/local`或是`node安装目录下`。



### 3.1 安装至GOPATH

您可以直接使用`go get`工具将`prometheus` 和`promtool`二进制文件下载并安装到您的`GOPATH`：

```shell
$ go get github.com/prometheus/prometheus/cmd/...
$ prometheus --config.file=your_config.yml
```

**但是**，当`go get`用于构建Prometheus时，Prometheus期望能够从`web/ui/static`下的本地文件系统目录中读取其Web资产`web/ui/templates`。为了找到这些资产，您必须从克隆的存储库的根目录运行Prometheus。还要注意，这些目录不包括新的实验性React UI，除非已使用`make assets`或显式构建了它`make build`。

可以在[此处](https://github.com/jwping/prometheus/blob/master/cmd/prometheus/prometheus.yml)找到上述配置文件的示例。



### 3.2 编译安装

您也可以自己克隆存储库并使用进行构建`make build`，它会连带Web资产一起进行打包编译，以便可以在任何地方运行Prometheus，而不在依赖于运行环境根目录下的``web/ui/templates``：

```shell
$ mkdir -p $GOPATH/src/github.com/prometheus
$ cd $GOPATH/src/github.com/prometheus
$ git clone https://github.com/prometheus/prometheus.git
$ cd prometheus
$ make build
$ ./prometheus --config.file=your_config.yml
```

Makefile提供了几个目标：

- *build*：构建`prometheus`和`promtool`二进制文件（包括Web资产一起进行构建和编译）
- *test*：运行测试
- *test-short*：运行简短测试
- *format*：格式化源代码
- *vet*：检查源代码是否存在常见错误
- *docker*：为当前容器构建一个docker容器（全架构构建）

### 3.3 Docker镜像构建

```shell
# 这里官方的Dockerfile有个小bug
# 请修改L8、L9
COPY .build/${OS}-${ARCH}/prometheus        /bin/prometheus
COPY .build/${OS}-${ARCH}/promtool          /bin/promtool
# 修改为如下↓
# 因为make构建出来的二进制可执行程序是放在当前路径下的，而不是.build下
COPY prometheus        /bin/prometheus
COPY promtool          /bin/promtool

docker build -t "prometheus-linux-amd64:v2.0" \
	-f ./Dockerfile \
	--build-arg ARCH="amd64" \
	--build-arg OS="linux" \
	./
```



## 4. 新增功能

### 4.1 配置文件监听重载

目前官方对于配置文件（对配置文件中指定的rule目录会进行监听，但rule规则文件不会监听）是不支持变动监听的，再每次文件变动后需要手动重载，官方支持两种方式：

```shell
hup := make(chan os.Signal, 1)
signal.Notify(hup, syscall.SIGHUP)
cancel := make(chan struct{})
g.Add(
	func() error {
		<-reloadReady.C
		for {
			select {
			case <-hup:
				if err := reloadConfig(cfg.configFile, logger, reloaders...); err != nil {
					level.Error(logger).Log("msg", "Error reloading config", "err", err)
				}
			case rc := <-webHandler.Reload():
				if err := reloadConfig(cfg.configFile, logger, reloaders...); err != nil {
					level.Error(logger).Log("msg", "Error reloading config", "err", err)
					rc <- err
				} else {
					rc <- nil
				}
			case <-cancel:
				return nil
			}
		}

	},
```

首先使用`signal.Notify`注册了一个信号监听器，注册捕捉信号`SIGHUP`，注册完成后使用`select`等待接收`hup`和`webHandler.Reload`通道传入数据后调用`reloadConfig`函数进行配置文件重载，这里`reloadConfig`函数接收三个参数：

* *cfg.configFile*: 配置文件路径
* *logger*: 日志输出实例
* *reloaders*: 这是一个函数数组在[cmd/prometheus/main.go#439](https://github.com/jwping/prometheus/blob/master/cmd/prometheus/main.go#L439)行定义，包括了web服务、日志采集、服务发现、rule文件发现控制器的重载方法

```shell
func reloadConfig(filename string, logger log.Logger, rls ...func(*config.Config) error) (err error) {
	level.Info(logger).Log("msg", "Loading configuration file", "filename", filename)

	defer func() {
		if err == nil {
			configSuccess.Set(1)
			configSuccessTime.SetToCurrentTime()
		} else {
			configSuccess.Set(0)
		}
	}()

	conf, err := config.LoadFile(filename)
	if err != nil {
		return errors.Wrapf(err, "couldn't load configuration (--config.file=%q)", filename)
	}

	failed := false
	for _, rl := range rls {
		if err := rl(conf); err != nil {
			level.Error(logger).Log("msg", "Failed to apply configuration", "err", err)
			failed = true
		}
	}
	if failed {
		return errors.Errorf("one or more errors occurred while applying the new configuration (--config.file=%q)", filename)
	}

	promql.SetDefaultEvaluationInterval(time.Duration(conf.GlobalConfig.EvaluationInterval))
	level.Info(logger).Log("msg", "Completed loading of configuration file", "filename", filename)
	return nil
}
```

`reloadConfig`函数主要是读取并解析配置文件，并将其作为参数传递到每个控制器的重载方法中。

#### 4.1.1 发送SIGHUP信号给应用程序的主进程

```shell
kill -HUP pid
kill -1 pid
```

通过向Prometheus进程发送`SIGHUP`信号使其进行配置文件重载，通过上面的源码分析可以看到其会接收`SIGHUP`信号。

#### 4.1.2 发送POST请求重载

```shell
curl -XPOST http://ip:port/-/reload
```

对于此种方法要注意在启动时加上`--web.enable-lifecycle`启动参数，同样在源码中可以看到，该请求会触发`webHandler.Reload`方法



#### 4.1.3 增加配置文件变动自动重载

上述两种方式为官方提供的重载方式，我们二次定制的版本会监听`configfile`以及`rule_files`中指定路径下的所有规则文件变化，并自动重载。

**请注意，当前版本移除了`--monitor`参数项**

使用`fsnotify.v1`包注册文件系统通知实现，当接收到文件系统发出的注册文件变动通知后调用`reloadConfig`函数，源码具体请查阅[cmd/prometheus/main.go#L771](https://github.com/jwping/prometheus/blob/master/cmd/prometheus/main.go#L771)



### 4.2 对每个node单独配置Params

在官方主分支中提供的版本仅支持在一个`jobname`中配置一个可选的URL参数列表，使得Prometheus进行数据采集时附带相应的URL参数，但并不支持对每个Target单独配置`Params`，我们扩展了这一点，目前仅提供对`static_configs`、`file_sd_configs`、`kubernetes_sd_configs`三种``Scrape``方式的支持。

```shell
static_configs:
- targets: ['localhost:9090']
  params:
    httplist: [ "www.baidu.com" ]
    portlist: [ "127.0.0.1:22" ]

---or---

[
  {
    "targets": [ "192.168.14.132:9100" ],
    "params": {
      "portlist": [ ":9100", "127.0.0.1:22" ],
      "httplist": [ "http://www.baidu.com" ]
    }
  }
]
```

对于`static_configs`和`file_sd_configs`可以使用如上方式进行单独指定

`kubernetes_sd_configs`方式采集数据无法对每个目标指定`params`项，所以我们通过对K8S资源添加`annotate`的方式来配置Params：

```shell
[root@master prometheus]# kubectl annotate node node1 portlist="127.0.0.1:22,:9100"
node/node1 annotated

[root@master prometheus]# kubectl describe node node1
Name:               node1
Roles:              ops-node
...
Annotations:        flannel.alpha.coreos.com/backend-data: {"VtepMAC":"56:04:c4:3d:38:ff"}
                    flannel.alpha.coreos.com/backend-type: vxlan
                    flannel.alpha.coreos.com/kube-subnet-manager: true
                    flannel.alpha.coreos.com/public-ip: 192.168.14.131
                    kubeadm.alpha.kubernetes.io/cri-socket: /var/run/dockershim.sock
                    node.alpha.kubernetes.io/ttl: 0
                    portlist: 127.0.0.1:22,:9100
                    volumes.kubernetes.io/controller-managed-attach-detach: true
CreationTimestamp:  Tue, 12 May 2020 13:38:52 +0800
...

$ kubectl annotate node qemu-node2 --overwrite httplist="https://www.baidu.com,https://aliyun.com,https://www.hao123.com"
node/qemu-node2 annotated
# --overwrite表示覆盖写入annotate，我这里已经有了httplist标签的值了，所以加入覆写参数
# 这里请特别注意url监控列表每一项请加入完整的http://或https://
```

当前版本下，我们对于`kubernetes_sd_configs`方式配置Params参数仅支持了`portlist`、`httplist`两类可选的URL附加参数，源码可见[discovery/kubernetes/node.go#187](https://github.com/jwping/prometheus/blob/master/discovery/kubernetes/node.go#L187)行，分别用于采集指定端口连通性和指定URL连通性，[详情可参考我们二次定制的node_export](https://github.com/jwping/node_exporter)

### 4.3 consul配置Params

```shell
$ cat join.sh 
curl -X PUT -d \
	'{"id": "node2","name": "node-exporter","address": "192.168.14.132","port": 9100,"tags": ["DEV"],"meta": {"portlist": ":22,127.0.0.1:9090", "httplist": "https://www.baidu.com,https://taobao.com"},"checks": [{"http": "http://192.168.14.132:9100/","interval": "5s"}]}' \
	http://192.168.14.132:8500/v1/agent/service/register
```

> 其中meta为需要添加入params中的自定义内容。
>
> 目前仅支持portlist、httplist两种



## 5. 修改说明

### 5.1 新增monitoring.go

```go
config/monitoring.go

// 该结构体主要定义了用户指定的规则文件列表和每次重载后移除的规则文件列表
type CMonitoring struct {
    Rules       []string
	Removefiles []string
    
	Channel chan struct{}
}

// 结构体实例化函数
func NewCMonitoring() *CMonitoring {...}

// 每次重载后将调用该函数来对用户配置文件指定的规则文件进行整理，已移除的文件加入到Removefiles中
func (c *CMonitoring) JudgeChange(files []string) {...}
```



### 5.2 修改main.go

```go
cmd/prometheus/main.go

// L50引用fsnotify包
fsnotify "gopkg.in/fsnotify/fsnotify.v1"

// L436增加CMonitoring实例化
cMonitoring := config.NewCMonitoring()

// L483增加Judge调用
go cMonitoring.JudgeChange(files)

// L768增加配置文件监听（包括程序主配置文件以及告警规则文件）
// 详见源码
```



### 5.3 修改node.go

```go
discovery/kubernetes/node.go

// L187增加getParams函数调用
getParams(tg)

// L211新增getParams函数
// 获取params后直接返回，指定了只在annotate中寻找portlist、httplist标签
func getParams(tg *targetgroup.Group) {}
```



### 5.4 修改consul.go

```go
discovery/consul/consul.go

// L506增加portlist、httplist meta过滤
// 参考源码，基本同node.go
```



### 5.5 修改targetgroup.go

```go
discovery/targetgroup/targetgroup.go

// L33
// 添加Params成员
Params url.Values

// L50
// 添加Params
Params url.Values `yaml:"params"`
// 下同
```



### 5.6 修改taget.go

```go
scrape/taget.go

// L208移除部分
// 详见源码

// L460增加对targetGroup的params判断，如果存在则使用target中定义的params
// 详见源码
```



### 5.7 增加主程序模板配置

```go
cmd/prometheus/prometheus.yml
cmd/prometheus/tfile.json
```

