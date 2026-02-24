 # Gin vs Hertz：Web 框架核心设计（结合本仓库代码）
 
 ## 目的
 我会先在你给的两个目录里快速定位它们的**入口、路由树/路由匹配、上下文(Context)、中间件链、启动与请求分发**这些“核心设计点”，然后对比总结 Gin vs Hertz 的设计取舍（性能、抽象边界、可扩展性）。
 
 ---
 
 ## 核心骨架（两者共通）
 1. **入口**：Server/Engine 接收请求，准备 Context
 2. **路由匹配**：按 HTTP Method 找到对应路由树，在树上按 Path 查找
 3. **绑定 handlers chain**：将命中的 handlers（中间件 + 最终 handler）挂到 Context
 4. **执行链**：Context 通过 `Next()` 线性执行 handlers，`Abort()` 终止后续
 5. **兜底**：未命中走 404/405（通常仍会让全局中间件参与）
 
 ---
 
 ## 设计取舍（差异点）
 - **Gin**：基于 `net/http`，I/O 与连接管理交给标准库；框架边界更小，更“库化”。
 - **Hertz**：自持网络/协议栈抽象（transport/protocol suite），路由/中间件模型与 Gin 同构，但能力边界更大（优雅退出、Hook、Conn/协议对象访问等）。
 
 ---
 
 ## 详细实现：从请求进入到 handler 执行（按调用顺序）
 
 ### Gin（`code/web/gin/gin/*`）
 
 #### 1) 请求入口：`Engine.ServeHTTP`（`gin/gin.go`）
 Context 从 `sync.Pool` 获取并复用：
 
 ```go
 ServeHTTP(w, req):
   routeTreesUpdated.Do(updateRouteTrees)
   c := pool.Get().(*Context)
   c.writermem.reset(w)
   c.Request = req
   c.reset()
   handleHTTPRequest(c)
   pool.Put(c)
 ```
 
 关键点：
 - `reset()` 必须清理上一次请求残留（handlers/index/缓存/params），否则复用会串数据。
 
 #### 2) 路由分发：`Engine.handleHTTPRequest`（`gin/gin.go`）
 ```go
 handleHTTPRequest(c):
   method := c.Request.Method
   path := normalizePath(UseEscapedPath/UseRawPath/RemoveExtraSlash)
 
   tree := trees.get(method)
   value := tree.root.getValue(path, c.params, c.skippedNodes, unescape)
 
   if value.handlers != nil:
     c.Params = *value.params
     c.handlers = value.handlers
     c.fullPath = value.fullPath
     c.Next()
     c.writermem.WriteHeaderNow()
     return
 
   // redirect / 405 / 404
 ```
 
 关键点：
 - **404/405 也执行中间件链**：Gin 的 `serveError` 会先 `c.Next()`，因此全局 middleware（日志、trace、metrics、recovery、统一错误包装）对 404/405 同样生效。
 - **405 判定**：扫描其它 method tree 是否命中同一路径，命中则返回 `Allow` header。
 
 #### 3) 中间件执行：`Context.Next/Abort`（`gin/context.go`）
 - `Next()`：`index++` 后 for 循环执行 `handlers[index](c)`
 - `Abort()`：把 `index` 置为 `abortIndex`，使后续 handlers 不再执行
 
 #### 4) 路由注册：`RouterGroup.Use/Group/combineHandlers`（`gin/routergroup.go`）
 - `Use(...)`：追加到 `group.Handlers`
 - 注册路由时：`combineHandlers()` 生成最终链：
   - `final = group.Handlers + routeHandlers`
 - `engine.addRoute(method, absolutePath, handlers)`：挂到对应 method tree，并维护 `maxParams/maxSections`
 
 #### 5) `Context.Copy()`（`gin/context.go`）
 因为 Context 会被 pool 回收复用，不能直接传 goroutine；`Copy()` 会拷贝 `Params/Keys` 等，生成可安全跨协程使用的副本。
 
 ---
 
 ### Hertz（`code/web/hertz/hertz/pkg/*`）
 
 #### 1) 请求入口：`route.Engine.ServeHTTP`（`pkg/route/engine.go`）
 ```go
 ServeHTTP(c, ctx):
   ctx.SetBinder(engine.binder)
   if engine.PanicHandler != nil:
     defer recv(ctx) // recover -> PanicHandler
 
   path := normalizePath(UseRawPath/RemoveExtraSlash/...)
   ensureParamsCapacity(&ctx.Params, engine.maxParams)
 
   value := methodTree.find(path, &ctx.Params, unescape)
   if value.handlers != nil:
     ctx.SetHandlers(value.handlers)
     ctx.SetFullPath(value.fullPath)
     ctx.Next(c)
     return
 
   // TSR / fixed-path / 405 / 404
 ```
 
 关键点：
 - `PanicHandler` 是 engine 级兜底（不止依赖 middleware）。
 - `RequestContext` 与 `protocol.Request/Response`、底层 `Conn` 绑定，能力边界更大。
 
 #### 2) 中间件执行：`RequestContext.Next/Abort`（`pkg/app/context.go`）
 - `Next(c)`：线性执行 `handlers[index](c, ctx)`
 - `Abort()`：将 index 设置为 `route/consts.AbortIndex`
 
 #### 3) 路由注册与 `*EX handlerName`（`pkg/route/routergroup.go`）
 - `Use/Group/combineHandlers` 与 Gin 同构：`final = group.Handlers + routeHandlers`
 - `GETEX/POSTEX/HandleEX`：允许显式传 `handlerName`，解决装饰器/匿名函数场景下函数名不可稳定获取的问题（便于 route dump、tracing、debug）。
 
 #### 4) 启动与优雅退出：`server.Hertz.Spin` + `Engine.Shutdown`
 - `Spin()`：`go h.Run()` 后等待 OS signal 或 Run 返回 error
 - `Shutdown(ctx)`：
   - 用 `status`（Running/Shutdown/Closed）做状态机
   - 并发执行 `OnShutdown` hooks 并等待超时/完成
   - 调用 `engine.transport.Shutdown(ctx)` 关闭 listener 并等待连接退出
 
 ---
 
 ## 详细实现：路由树（Radix/压缩前缀树）与参数提取要点
 - **节点类型（概念）**：静态段、参数段（`:id`）、catch-all（`*filepath`）
 - **匹配优先级**：通常静态 > param > catch-all（减少歧义）
 - **参数写入复用**：
   - Gin：`getValue(..., c.params, c.skippedNodes, ...)` 产出 `value.params`，再 `c.Params = *value.params`
   - Hertz：`find(..., &ctx.Params, ...)` 直接在 `ctx.Params` 上复用追加
 - **`tsr`（Trailing Slash Redirect）**：查找时产出 “是否只差一个 / 就命中”，配合 `RedirectTrailingSlash` 做 `/foo` <-> `/foo/` 重定向
 - **`skippedNodes`（Gin）**：用于带参数匹配时的回溯/跳分支优化，减少错误分支导致的重复遍历
 
 ---
 
 ## 推荐阅读顺序（最快建立心智模型）
 
 ### Gin
 - `gin/gin.go`：`ServeHTTP`、`handleHTTPRequest`
 - `gin/routergroup.go`：`Use/Group/combineHandlers`、`handle/addRoute`
 - `gin/context.go`：`reset/Next/Abort/Copy`
 - `gin/tree.go`：路由树（注册/查找）细节
 
 ### Hertz
 - `pkg/route/engine.go`：`Engine` + `ServeHTTP` + `Shutdown`
 - `pkg/app/context.go`：`RequestContext.Next/Abort` + Conn/协议能力
 - `pkg/route/routergroup.go`：`Use/Group/combineHandlers` + `*EX handlerName`
 - `pkg/app/server/hertz.go`：`Spin`（启动、信号、优雅退出入口）
