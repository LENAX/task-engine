module test-task-engine

go 1.24.2

// 关键：将task-engine的模块名映射到本地绝对路径
replace github.com/stevelan1995/task-engine => /Users/stevelan/Desktop/projects/task-engine/task-engine

// 声明依赖（版本可随便写，因为replace会覆盖）
require github.com/stevelan1995/task-engine v1.0.0

require gopkg.in/yaml.v3 v3.0.1 // indirect
