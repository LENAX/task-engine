package unit

import (
	"context"
	"testing"

	"github.com/LENAX/task-engine/pkg/core/task"
)

// TestTaskContext_GetUpstreamResult 测试获取指定上游任务结果
func TestTaskContext_GetUpstreamResult(t *testing.T) {
	params := map[string]interface{}{
		"_cached_FetchStockList": map[string]interface{}{
			"stock_codes": []string{"000001", "000002", "000003"},
			"count":       3,
		},
		"_cached_FetchAPIList": map[string]interface{}{
			"api_names": []string{"stock_basic", "daily"},
		},
		"other_param": "value",
	}

	tc := task.NewTaskContext(context.Background(), "task-1", "TestTask", "wf-1", "wfi-1", params)

	// 测试获取存在的上游结果
	result := tc.GetUpstreamResult("FetchStockList")
	if result == nil {
		t.Fatal("期望获取到 FetchStockList 的结果，实际为 nil")
	}
	if result["count"] != 3 {
		t.Errorf("期望 count=3，实际 count=%v", result["count"])
	}

	// 测试获取不存在的上游结果
	result = tc.GetUpstreamResult("NotExist")
	if result != nil {
		t.Errorf("期望获取不存在的上游结果返回 nil，实际为 %v", result)
	}
}

// TestTaskContext_GetAllUpstreamResults 测试获取所有上游任务结果
func TestTaskContext_GetAllUpstreamResults(t *testing.T) {
	params := map[string]interface{}{
		"_cached_Task1": map[string]interface{}{"data": "result1"},
		"_cached_Task2": map[string]interface{}{"data": "result2"},
		"_cached_Task3": map[string]interface{}{"data": "result3"},
		"normal_param":  "value",
	}

	tc := task.NewTaskContext(context.Background(), "task-1", "TestTask", "wf-1", "wfi-1", params)

	results := tc.GetAllUpstreamResults()
	if len(results) != 3 {
		t.Errorf("期望获取 3 个上游结果，实际获取 %d 个", len(results))
	}

	if results["Task1"]["data"] != "result1" {
		t.Errorf("期望 Task1.data='result1'，实际为 %v", results["Task1"]["data"])
	}
}

// TestTaskContext_GetUpstreamValue 测试从上游结果获取字段值
func TestTaskContext_GetUpstreamValue(t *testing.T) {
	params := map[string]interface{}{
		"_cached_FetchData": map[string]interface{}{
			"name":  "test",
			"count": 100,
		},
	}

	tc := task.NewTaskContext(context.Background(), "task-1", "TestTask", "wf-1", "wfi-1", params)

	// 测试获取存在的字段
	val := tc.GetUpstreamValue("FetchData", "name")
	if val != "test" {
		t.Errorf("期望 name='test'，实际为 %v", val)
	}

	// 测试获取不存在的字段
	val = tc.GetUpstreamValue("FetchData", "not_exist")
	if val != nil {
		t.Errorf("期望不存在的字段返回 nil，实际为 %v", val)
	}

	// 测试获取不存在的任务的字段
	val = tc.GetUpstreamValue("NotExistTask", "name")
	if val != nil {
		t.Errorf("期望不存在的任务字段返回 nil，实际为 %v", val)
	}
}

// TestTaskContext_GetUpstreamString 测试获取上游字符串字段
func TestTaskContext_GetUpstreamString(t *testing.T) {
	params := map[string]interface{}{
		"_cached_Task1": map[string]interface{}{
			"name":   "test_name",
			"number": 123,
		},
	}

	tc := task.NewTaskContext(context.Background(), "task-1", "TestTask", "wf-1", "wfi-1", params)

	// 测试获取字符串
	str := tc.GetUpstreamString("Task1", "name")
	if str != "test_name" {
		t.Errorf("期望 name='test_name'，实际为 %v", str)
	}

	// 测试获取非字符串（会转换）
	str = tc.GetUpstreamString("Task1", "number")
	if str != "123" {
		t.Errorf("期望 number='123'，实际为 %v", str)
	}

	// 测试获取不存在的字段
	str = tc.GetUpstreamString("Task1", "not_exist")
	if str != "" {
		t.Errorf("期望不存在的字段返回空字符串，实际为 %v", str)
	}
}

// TestTaskContext_GetUpstreamInt 测试获取上游整数字段
func TestTaskContext_GetUpstreamInt(t *testing.T) {
	params := map[string]interface{}{
		"_cached_Task1": map[string]interface{}{
			"count":   100,
			"count64": int64(200),
			"countF":  float64(300),
			"name":    "not_int",
		},
	}

	tc := task.NewTaskContext(context.Background(), "task-1", "TestTask", "wf-1", "wfi-1", params)

	// 测试获取 int
	val, err := tc.GetUpstreamInt("Task1", "count")
	if err != nil || val != 100 {
		t.Errorf("期望 count=100，实际为 %v, err=%v", val, err)
	}

	// 测试获取 int64
	val, err = tc.GetUpstreamInt("Task1", "count64")
	if err != nil || val != 200 {
		t.Errorf("期望 count64=200，实际为 %v, err=%v", val, err)
	}

	// 测试获取 float64
	val, err = tc.GetUpstreamInt("Task1", "countF")
	if err != nil || val != 300 {
		t.Errorf("期望 countF=300，实际为 %v, err=%v", val, err)
	}

	// 测试获取非整数
	_, err = tc.GetUpstreamInt("Task1", "name")
	if err == nil {
		t.Error("期望获取非整数字段返回错误")
	}

	// 测试获取不存在的字段
	_, err = tc.GetUpstreamInt("Task1", "not_exist")
	if err == nil {
		t.Error("期望获取不存在的字段返回错误")
	}
}

// TestTaskContext_GetUpstreamStringSlice 测试获取上游字符串切片
func TestTaskContext_GetUpstreamStringSlice(t *testing.T) {
	params := map[string]interface{}{
		"_cached_Task1": map[string]interface{}{
			"codes_str":       []string{"a", "b", "c"},
			"codes_interface": []interface{}{"x", "y", "z"},
			"not_slice":       "single_value",
		},
	}

	tc := task.NewTaskContext(context.Background(), "task-1", "TestTask", "wf-1", "wfi-1", params)

	// 测试 []string
	slice := tc.GetUpstreamStringSlice("Task1", "codes_str")
	if len(slice) != 3 || slice[0] != "a" {
		t.Errorf("期望 codes_str=['a','b','c']，实际为 %v", slice)
	}

	// 测试 []interface{}
	slice = tc.GetUpstreamStringSlice("Task1", "codes_interface")
	if len(slice) != 3 || slice[0] != "x" {
		t.Errorf("期望 codes_interface=['x','y','z']，实际为 %v", slice)
	}

	// 测试非切片
	slice = tc.GetUpstreamStringSlice("Task1", "not_slice")
	if slice != nil {
		t.Errorf("期望非切片返回 nil，实际为 %v", slice)
	}
}

// TestTaskContext_GetSubTaskResults 测试获取子任务结果
func TestTaskContext_GetSubTaskResults(t *testing.T) {
	params := map[string]interface{}{
		"_cached_TemplateTask": map[string]interface{}{
			"subtask_results": []map[string]interface{}{
				{
					"task_id":   "subtask-1",
					"task_name": "子任务1",
					"status":    "Success",
					"result": map[string]interface{}{
						"data": "result1",
					},
					"error": "",
				},
				{
					"task_id":   "subtask-2",
					"task_name": "子任务2",
					"status":    "Failed",
					"result":    nil,
					"error":     "执行失败",
				},
				{
					"task_id":   "subtask-3",
					"task_name": "子任务3",
					"status":    "Success",
					"result": map[string]interface{}{
						"data": "result3",
					},
					"error": "",
				},
			},
			"subtask_count":          3,
			"all_subtasks_succeeded": false,
		},
	}

	tc := task.NewTaskContext(context.Background(), "task-1", "TestTask", "wf-1", "wfi-1", params)

	// 测试获取所有子任务结果
	results := tc.GetSubTaskResults()
	if len(results) != 3 {
		t.Fatalf("期望获取 3 个子任务结果，实际获取 %d 个", len(results))
	}

	// 验证第一个子任务
	if results[0].TaskID != "subtask-1" {
		t.Errorf("期望 results[0].TaskID='subtask-1'，实际为 %v", results[0].TaskID)
	}
	if !results[0].IsSuccess() {
		t.Error("期望 results[0] 成功")
	}

	// 验证第二个子任务（失败）
	if results[1].Status != "Failed" {
		t.Errorf("期望 results[1].Status='Failed'，实际为 %v", results[1].Status)
	}
	if results[1].Error != "执行失败" {
		t.Errorf("期望 results[1].Error='执行失败'，实际为 %v", results[1].Error)
	}
}

// TestTaskContext_GetSubTaskResults_InterfaceSlice 测试子任务结果为 []interface{} 类型
func TestTaskContext_GetSubTaskResults_InterfaceSlice(t *testing.T) {
	// 模拟 JSON 反序列化后的类型（[]interface{} 而非 []map[string]interface{}）
	params := map[string]interface{}{
		"_cached_TemplateTask": map[string]interface{}{
			"subtask_results": []interface{}{
				map[string]interface{}{
					"task_id":   "subtask-1",
					"task_name": "子任务1",
					"status":    "Success",
					"result": map[string]interface{}{
						"data": "result1",
					},
				},
			},
			"subtask_count":          1,
			"all_subtasks_succeeded": true,
		},
	}

	tc := task.NewTaskContext(context.Background(), "task-1", "TestTask", "wf-1", "wfi-1", params)

	results := tc.GetSubTaskResults()
	if len(results) != 1 {
		t.Fatalf("期望获取 1 个子任务结果，实际获取 %d 个", len(results))
	}
	if results[0].TaskID != "subtask-1" {
		t.Errorf("期望 TaskID='subtask-1'，实际为 %v", results[0].TaskID)
	}
}

// TestTaskContext_GetSuccessfulSubTaskResults 测试获取成功的子任务结果
func TestTaskContext_GetSuccessfulSubTaskResults(t *testing.T) {
	params := map[string]interface{}{
		"_cached_TemplateTask": map[string]interface{}{
			"subtask_results": []map[string]interface{}{
				{"task_id": "s1", "task_name": "子任务1", "status": "Success", "result": map[string]interface{}{"v": 1}},
				{"task_id": "s2", "task_name": "子任务2", "status": "Failed", "error": "err"},
				{"task_id": "s3", "task_name": "子任务3", "status": "Success", "result": map[string]interface{}{"v": 3}},
			},
			"subtask_count":          3,
			"all_subtasks_succeeded": false,
		},
	}

	tc := task.NewTaskContext(context.Background(), "task-1", "TestTask", "wf-1", "wfi-1", params)

	successResults := tc.GetSuccessfulSubTaskResults()
	if len(successResults) != 2 {
		t.Errorf("期望获取 2 个成功的子任务结果，实际获取 %d 个", len(successResults))
	}
}

// TestTaskContext_GetFailedSubTaskResults 测试获取失败的子任务结果
func TestTaskContext_GetFailedSubTaskResults(t *testing.T) {
	params := map[string]interface{}{
		"_cached_TemplateTask": map[string]interface{}{
			"subtask_results": []map[string]interface{}{
				{"task_id": "s1", "task_name": "子任务1", "status": "Success", "result": map[string]interface{}{}},
				{"task_id": "s2", "task_name": "子任务2", "status": "Failed", "error": "err1"},
				{"task_id": "s3", "task_name": "子任务3", "status": "Failed", "error": "err2"},
			},
			"subtask_count":          3,
			"all_subtasks_succeeded": false,
		},
	}

	tc := task.NewTaskContext(context.Background(), "task-1", "TestTask", "wf-1", "wfi-1", params)

	failedResults := tc.GetFailedSubTaskResults()
	if len(failedResults) != 2 {
		t.Errorf("期望获取 2 个失败的子任务结果，实际获取 %d 个", len(failedResults))
	}
}

// TestTaskContext_AllSubTasksSucceeded 测试检查所有子任务是否成功
func TestTaskContext_AllSubTasksSucceeded(t *testing.T) {
	// 全部成功
	params1 := map[string]interface{}{
		"_cached_Template": map[string]interface{}{
			"all_subtasks_succeeded": true,
		},
	}
	tc1 := task.NewTaskContext(context.Background(), "t1", "T1", "wf", "wfi", params1)
	if !tc1.AllSubTasksSucceeded() {
		t.Error("期望 AllSubTasksSucceeded() 返回 true")
	}

	// 部分失败
	params2 := map[string]interface{}{
		"_cached_Template": map[string]interface{}{
			"all_subtasks_succeeded": false,
		},
	}
	tc2 := task.NewTaskContext(context.Background(), "t2", "T2", "wf", "wfi", params2)
	if tc2.AllSubTasksSucceeded() {
		t.Error("期望 AllSubTasksSucceeded() 返回 false")
	}

	// 无子任务结果
	params3 := map[string]interface{}{}
	tc3 := task.NewTaskContext(context.Background(), "t3", "T3", "wf", "wfi", params3)
	if tc3.AllSubTasksSucceeded() {
		t.Error("期望无子任务结果时 AllSubTasksSucceeded() 返回 false")
	}
}

// TestTaskContext_GetSubTaskCount 测试获取子任务数量
func TestTaskContext_GetSubTaskCount(t *testing.T) {
	// int 类型
	params1 := map[string]interface{}{
		"_cached_Template": map[string]interface{}{
			"subtask_count": 5,
		},
	}
	tc1 := task.NewTaskContext(context.Background(), "t1", "T1", "wf", "wfi", params1)
	if tc1.GetSubTaskCount() != 5 {
		t.Errorf("期望 GetSubTaskCount()=5，实际为 %d", tc1.GetSubTaskCount())
	}

	// float64 类型（JSON 反序列化）
	params2 := map[string]interface{}{
		"_cached_Template": map[string]interface{}{
			"subtask_count": float64(10),
		},
	}
	tc2 := task.NewTaskContext(context.Background(), "t2", "T2", "wf", "wfi", params2)
	if tc2.GetSubTaskCount() != 10 {
		t.Errorf("期望 GetSubTaskCount()=10，实际为 %d", tc2.GetSubTaskCount())
	}
}

// TestTaskContext_ExtractFromSubTasks 测试从子任务提取字段
func TestTaskContext_ExtractFromSubTasks(t *testing.T) {
	params := map[string]interface{}{
		"_cached_FetchAPIDetailAll": map[string]interface{}{
			"subtask_results": []map[string]interface{}{
				{
					"task_id": "fetch-1", "task_name": "获取API1", "status": "Success",
					"result": map[string]interface{}{
						"api_metadata": map[string]interface{}{
							"id": "api-001", "name": "stock_basic",
						},
					},
				},
				{
					"task_id": "fetch-2", "task_name": "获取API2", "status": "Success",
					"result": map[string]interface{}{
						"api_metadata": map[string]interface{}{
							"id": "api-002", "name": "daily",
						},
					},
				},
				{
					"task_id": "fetch-3", "task_name": "获取API3", "status": "Failed",
					"error": "网络超时",
				},
			},
			"subtask_count":          3,
			"all_subtasks_succeeded": false,
		},
	}

	tc := task.NewTaskContext(context.Background(), "task-1", "SaveAPIMetadata", "wf-1", "wfi-1", params)

	// 测试 ExtractFromSubTasks
	values := tc.ExtractFromSubTasks("api_metadata")
	if len(values) != 2 {
		t.Errorf("期望提取 2 个 api_metadata，实际提取 %d 个", len(values))
	}

	// 测试 ExtractMapsFromSubTasks
	maps := tc.ExtractMapsFromSubTasks("api_metadata")
	if len(maps) != 2 {
		t.Fatalf("期望提取 2 个 api_metadata map，实际提取 %d 个", len(maps))
	}
	if maps[0]["name"] != "stock_basic" {
		t.Errorf("期望 maps[0]['name']='stock_basic'，实际为 %v", maps[0]["name"])
	}
	if maps[1]["name"] != "daily" {
		t.Errorf("期望 maps[1]['name']='daily'，实际为 %v", maps[1]["name"])
	}
}

// TestTaskContext_ExtractStringsFromSubTasks 测试从子任务提取字符串字段
func TestTaskContext_ExtractStringsFromSubTasks(t *testing.T) {
	params := map[string]interface{}{
		"_cached_ParseList": map[string]interface{}{
			"subtask_results": []map[string]interface{}{
				{"task_id": "p1", "status": "Success", "result": map[string]interface{}{"code": "000001"}},
				{"task_id": "p2", "status": "Success", "result": map[string]interface{}{"code": "000002"}},
				{"task_id": "p3", "status": "Failed", "error": "err"},
			},
			"subtask_count":          3,
			"all_subtasks_succeeded": false,
		},
	}

	tc := task.NewTaskContext(context.Background(), "task-1", "AggregateTask", "wf-1", "wfi-1", params)

	codes := tc.ExtractStringsFromSubTasks("code")
	if len(codes) != 2 {
		t.Errorf("期望提取 2 个 code，实际提取 %d 个", len(codes))
	}
	if codes[0] != "000001" || codes[1] != "000002" {
		t.Errorf("期望 codes=['000001','000002']，实际为 %v", codes)
	}
}

// TestSubTaskResult_Methods 测试 SubTaskResult 的方法
func TestSubTaskResult_Methods(t *testing.T) {
	result := task.SubTaskResult{
		TaskID:   "sub-1",
		TaskName: "子任务1",
		Status:   "Success",
		Result: map[string]interface{}{
			"data": map[string]interface{}{
				"key": "value",
			},
			"count": 100,
		},
		Error: "",
	}

	// 测试 IsSuccess
	if !result.IsSuccess() {
		t.Error("期望 IsSuccess() 返回 true")
	}

	// 测试 GetResultValue
	val := result.GetResultValue("count")
	if val != 100 {
		t.Errorf("期望 count=100，实际为 %v", val)
	}

	// 测试 GetResultMap
	dataMap := result.GetResultMap("data")
	if dataMap == nil || dataMap["key"] != "value" {
		t.Errorf("期望 data['key']='value'，实际为 %v", dataMap)
	}

	// 测试获取不存在的字段
	if result.GetResultValue("not_exist") != nil {
		t.Error("期望不存在的字段返回 nil")
	}
	if result.GetResultMap("not_exist") != nil {
		t.Error("期望不存在的 map 字段返回 nil")
	}

	// 测试失败的 SubTaskResult
	failedResult := task.SubTaskResult{
		TaskID: "sub-2",
		Status: "Failed",
		Error:  "执行失败",
	}
	if failedResult.IsSuccess() {
		t.Error("期望失败的 SubTaskResult.IsSuccess() 返回 false")
	}
}

// TestTaskContext_EmptyParams 测试空参数场景
func TestTaskContext_EmptyParams(t *testing.T) {
	tc := task.NewTaskContext(context.Background(), "task-1", "TestTask", "wf-1", "wfi-1", nil)

	// 所有方法应该安全返回
	if tc.GetUpstreamResult("any") != nil {
		t.Error("空参数时 GetUpstreamResult 应返回 nil")
	}
	if len(tc.GetAllUpstreamResults()) != 0 {
		t.Error("空参数时 GetAllUpstreamResults 应返回空 map")
	}
	if len(tc.GetSubTaskResults()) != 0 {
		t.Error("空参数时 GetSubTaskResults 应返回空切片")
	}
	if tc.AllSubTasksSucceeded() {
		t.Error("空参数时 AllSubTasksSucceeded 应返回 false")
	}
	if tc.GetSubTaskCount() != 0 {
		t.Error("空参数时 GetSubTaskCount 应返回 0")
	}
}
