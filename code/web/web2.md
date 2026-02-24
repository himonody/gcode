 
 
 ## Gin：请求主链路是怎么跑起来的（gin/gin.go）
 
 ### 1) ServeHTTP：从 `sync.Pool` 取 Context，跑完再放回去
 核心目的：**避免每次请求分配 `Context`/切片/临时对象**。
 
 流程（对应 Engine.ServeHTTP）：
 1. `engine.routeTreesUpdated.Do(engine.updateRouteTrees)`  
    - 只执行一次（`sync.Once`），把路由树里被转义的字符做统一修正（你仓库里是把 `\:` 之类的替换回来）。
 2. `c := engine.pool.Get().(*Context)`  
 3. `c.writermem.reset(w)`：把 responseWriter 绑定到当前请求的 `http.ResponseWriter` 
 4. `c.Request = req` 
 5. `c.reset()`：把上次请求残留状态清掉（`handlers/index/Keys/Errors/Params/...`）
 6. `engine.handleHTTPRequest(c)`：真正的路由匹配与中间件执行
 7. `engine.pool.Put(c)`：复用 Context
 
 ---
 
 ### 2) handleHTTPRequest：按 method 找树、按 path 找节点、拿到 handlers 链执行
 对应 Engine.handleHTTPRequest(c)：
 
 #### a. 先确定用于匹配的 path
 - 默认用 `c.Request.URL.Path`
 - 如果配置了：
   - `UseEscapedPath`：用 `URL.EscapedPath()`（是否 `UnescapePathValues` 由配置决定）
   - `UseRawPath`：用 `URL.RawPath` 
 - `RemoveExtraSlash`：会 `cleanPath` 归一化
 
 #### b. 按 HTTP method 找到对应的路由树
 - `engine.trees` 是 `methodTrees` 
 - 遍历 `engine.trees` 找 `tree.method == httpMethod` 
 
 #### c. 在 radix tree 上查找
 - `value := root.getValue(rPath, c.params, c.skippedNodes, unescape)` 
 - 命中时：
   - `c.Params = *value.params`（路径参数）
   - `c.handlers = value.handlers`（最终中间件链：group + route）
   - `c.fullPath = value.fullPath`（如 `/user/:id`）
   - `c.Next()` 执行整条链
   - `c.writermem.WriteHeaderNow()` 保证 headers 落地
 
 #### d. 未命中：处理重定向、405、404
 - **TSR（尾斜杠）重定向**：`value.tsr && engine.RedirectTrailingSlash` 
 - **FixedPath 重定向**：`engine.RedirectFixedPath && redirectFixedPath(...)`（大小写/清理后的 path 尝试）
 - **405 Method Not Allowed**（如果 `HandleMethodNotAllowed`）：
   - 扫描其它 method tree，看同 path 是否有 handlers
   - 有则设置 `Allow` header，走 `engine.allNoMethod` 链并 `serveError(405, ...)`
 - 否则：`engine.allNoRoute` + `serveError(404, ...)`
 
 > serveError 的设计点：**即使是 404/405，也会先 `c.Next()` 让全局 middleware 有机会执行**（例如日志、metrics、recovery、统一错误包装），如果 middleware 已经写了响应，就不再写默认 body。
 
 ---
 
 ## Gin：路由注册与中间件合并（gin/routergroup.go + gin/gin.go:addRoute）
 
 ### 1) Use
 - RouterGroup.Use(mw...) 只是把 middleware append 到 group.Handlers
 - 重要点：**group 的中间件不会“立即生效”到某个路由**，而是在注册路由时被合并进最终 handlers 链。
 
 ### 2) Group
 - Group(relativePath, handlers...) 创建子组：
   - Handlers: group.combineHandlers(handlers)
   - `basePath: joinPaths(group.basePath, relativePath)` 
 - 所以：**子组天然继承父组中间件 + 自己新增的中间件**。
 
 ### 3) combineHandlers
 - 合并规则：`merged = parentGroupHandlers + routeHandlers` 
 - 这里还有一个强约束：`finalSize < abortIndex`  
   因为 Gin 用 `int8 index` 做 Next() 的游标，`abortIndex` 是一个很大的 int8 值（避免溢出/冲突）。
 
 ### 4) addRoute（在 gin/gin.go）
 - engine.addRoute(method, absolutePath, handlers)
 - 找/建 method tree，然后 root.addRoute(path, handlers)
 - 同时更新 `engine.maxParams/maxSections`（用于后续 Context 分配/复用时的容量规划）
 
 ---
 
 ## Gin：Context 执行模型（gin/context.go）
 
 ### 1) `reset`：复用的关键
 `Context.reset()` 会把这些字段恢复到“刚取出时”的干净状态：
 - `Writer` 指回 `writermem` 
 - `Params` 切片清空（保留容量）
 - `handlers=nil`, `index=-1`, `fullPath=""` 
 - `Keys=nil`, `Errors` 清空
 - query/form cache 清空
 - `*c.params`、`*c.skippedNodes` 清空（这俩是指针指向 Engine 预分配的切片，用来减少分配）
 
 ### 2) Next：线性链执行（零额外抽象）
 - `c.index++` 
 - `for c.index < len(c.handlers)`：
   - 调用当前 handler
   - `c.index++` 
 - middleware 内调用 `c.Next()` 就会“继续跑后面的 handler”，返回后可以做“后置逻辑”（典型洋葱模型）。
 
 ### 3) Abort
 - `c.index = abortIndex` 
 - 之后 Next() 的 loop 条件不成立，链路终止。
 
 ### 4) Copy
 - 用于把 Context 安全传到 goroutine（因为原 Context 会被 pool 回收复用）
 - 行为：
   - 拷贝 `Params` 
   - clone `Keys` 
   - 把 `Writer` 置为可用但不再绑定真实 `ResponseWriter` 
 
 ---
 
 ## Gin：tree.go（路由树）大概怎么工作（不展开所有细节）
 你现在理解 handleHTTPRequest 就够了；继续深入 tree.go 时，建议重点看两件事：
 - **注册**：node.addRoute(path, handlers) 如何把静态段、`:param`、`*catchAll` 插到树里
 - **查找**：`node.getValue(path, params, skippedNodes, unescape)` 如何：
   - 走压缩前缀匹配（radix）
   - 遇到参数节点时把值写入 `Params`（尽量复用切片）
   - 设置 `tsr`（用于建议是否需要尾斜杠重定向）
 
 ---
 
 ## Hertz：同一套“路由树 + handlers chain”，但请求/响应/网络栈是自持的
 
 ### 1) route.Engine.ServeHTTP（pkg/route/engine.go）
 和 Gin 的 handleHTTPRequest 同构，但注意几个关键差异：
 
 - 输入不是 `http.ResponseWriter/*http.Request`，而是：
   - `c context.Context` 
   - ctx *app.RequestContext（里面封装 Hertz 的 `protocol.Request/Response`）
 - 取 path：`rPath := string(ctx.Request.URI().Path())` 
 - 命中后：
   - ctx.SetHandlers(value.handlers)
   - ctx.SetFullPath(value.fullPath)
   - ctx.Next(c)
 - 未命中处理：
   - 同样有 TSR / fixed path / 405 / 404
 - 额外能力：
   - `PanicHandler`：`defer engine.recv(ctx)` 做 recover（Gin 通常靠 Recovery middleware；Hertz 提供了 engine 级兜底点）
   - `ctx.Params` 容量不足会扩容（因为 Params 在 handler 中可能被重置/替换）
 
 ### 2) app.RequestContext.Next/Abort（pkg/app/context.go）
 - Next(c)：`index++` + for 循环执行 `handlers[index](c, ctx)` 
 - Abort()：设置 index 到 `route/consts.AbortIndex` 
 
 另外 Hertz 的 RequestContext 还带：
 - GetConn()/GetReader()/GetWriter()：能触达底层连接（对性能/协议扩展很关键）
 - binder、traceinfo、hijack handler 等，属于“更平台化”的 Context
 
 ### 3) route.RouterGroup（pkg/route/routergroup.go）
 和 Gin 基本一致：
 - `Use/Group/combineHandlers` 
 - 但多了 `GETEX/POSTEX/...`：
   - 在 handler 被装饰、或匿名函数场景下，**无法稳定获取函数名**，Hertz 允许你显式传 `handlerName`，用于路由信息、tracing、debug。
 
 ### 4) 启动/优雅退出（pkg/app/server/hertz.go + route.Engine.Shutdown）
 - `server.Hertz.Spin()`：
   - go h.Run() 启动
   - 阻塞等待 signal 或 Run 返回 error
   - 正常信号 -> `h.Shutdown(ctx)` 
 - Engine.Shutdown(ctx)（pkg/route/engine.go）：
   - 检查状态（Running -> Shutdown）
   - 并发触发 `OnShutdown` hooks，并等待到 timeout
   - `engine.transport.Shutdown(ctx)`：关闭 listener + 等连接退出（这就是 Hertz “自持网络栈”的落点）
 
 ---
 
 ## 任务状态
 - 已说明：你列出的每个关键文件/函数在框架内部是如何协作实现“请求分发 + 路由匹配 + middleware 链 + Context 复用 + 启停”的。
