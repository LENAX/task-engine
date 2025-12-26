package router

import (
    "net/http"

    "github.com/stevelan1995/task-engine/pkg/engine"
)

// InitRouter 初始化HTTP路由
func InitRouter(eng *engine.Engine) *http.ServeMux {
    mux := http.NewServeMux()

    // 健康检查接口
    mux.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
        w.WriteHeader(http.StatusOK)
        w.Write([]byte("{\"code\":0,\"message\":\"success\",\"data\":\"engine is running\"}"))
    })

    // TODO: 注册其他业务接口
    return mux
}
