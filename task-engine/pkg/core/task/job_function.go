package task

import (
	"context"
	"encoding/json"
	"fmt"
	"reflect"
	"strconv"
)

// JobFunctionState 表示Job函数的执行状态和可选结果或错误
type JobFunctionState struct {
	Status string      // "Success" or "Failed"
	Data   interface{} // optional: any return value (on success)
	Error  error       // optional: error info (on failure)
}

// JobFunctionType 是调度用统一函数签名，负责包裹用户逻辑，并异步通知状态
// 参数通过TaskContext传递，提供类型安全的API访问Task信息
type JobFunctionType func(ctx *TaskContext) <-chan JobFunctionState

// WrapJobFunc 将任意函数包装为JobFunctionType（对外导出）
// 支持两种函数签名：
//  1. 新方式：func(ctx *TaskContext) (result, error) 或 func(ctx *TaskContext) error
//  2. 旧方式（兼容）：func(ctx context.Context, ...) (result, error) 或 func(ctx context.Context, ...) error
//     旧方式的额外参数从TaskContext.Params中获取
func WrapJobFunc(fn interface{}) (JobFunctionType, error) {
	fnValue := reflect.ValueOf(fn)
	fnType := fnValue.Type()

	// 检查是否为函数类型
	if fnType.Kind() != reflect.Func {
		return nil, fmt.Errorf("参数必须是函数类型，当前类型: %v", fnType.Kind())
	}

	// 检查参数数量
	if fnType.NumIn() == 0 {
		return nil, fmt.Errorf("函数至少需要一个参数")
	}

	// 检查返回值
	numOut := fnType.NumOut()
	if numOut == 0 {
		return nil, fmt.Errorf("函数必须至少返回一个error")
	}
	lastOutType := fnType.Out(numOut - 1)
	errorType := reflect.TypeOf((*error)(nil)).Elem()
	if !lastOutType.Implements(errorType) {
		return nil, fmt.Errorf("函数最后一个返回值必须是error，当前类型: %v", lastOutType)
	}

	firstParamType := fnType.In(0)
	taskContextType := reflect.TypeOf((*TaskContext)(nil))

	// 检查第一个参数是否为*TaskContext（新方式）
	if firstParamType == taskContextType {
		return wrapTaskContextFunc(fnValue, fnType, numOut), nil
	}

	// 检查第一个参数是否为context.Context（旧方式，兼容）
	contextType := reflect.TypeOf((*context.Context)(nil)).Elem()
	if firstParamType.Implements(contextType) || firstParamType == contextType {
		return wrapLegacyFunc(fnValue, fnType, numOut), nil
	}

	return nil, fmt.Errorf("函数第一个参数必须是*TaskContext或context.Context，当前类型: %v", firstParamType)
}

// wrapTaskContextFunc 包装使用TaskContext的函数（新方式）
func wrapTaskContextFunc(fnValue reflect.Value, fnType reflect.Type, numOut int) JobFunctionType {
	return func(taskCtx *TaskContext) <-chan JobFunctionState {
		stateCh := make(chan JobFunctionState, 1)
		go func() {
			defer close(stateCh)

			// 调用函数，只传入TaskContext
			args := []reflect.Value{reflect.ValueOf(taskCtx)}
			results := fnValue.Call(args)

			// 处理返回值
			var result interface{}
			var err error

			if numOut == 1 {
				// 只返回error
				if results[0].IsNil() {
					err = nil
				} else {
					err = results[0].Interface().(error)
				}
			} else {
				// 返回(result, error)
				result = results[0].Interface()
				if !results[1].IsNil() {
					err = results[1].Interface().(error)
				}
			}

			if err != nil {
				stateCh <- JobFunctionState{
					Status: "Failed",
					Error:  err,
				}
				return
			}

			stateCh <- JobFunctionState{
				Status: "Success",
				Data:   result,
			}
		}()
		return stateCh
	}
}

// wrapLegacyFunc 包装使用context.Context的函数（旧方式，兼容）
func wrapLegacyFunc(fnValue reflect.Value, fnType reflect.Type, numOut int) JobFunctionType {
	return func(taskCtx *TaskContext) <-chan JobFunctionState {
		stateCh := make(chan JobFunctionState, 1)
		go func() {
			defer close(stateCh)

			// 准备函数参数
			args := make([]reflect.Value, fnType.NumIn())
			args[0] = reflect.ValueOf(taskCtx.Context()) // 第一个参数是context.Context

			// 处理其余参数（从TaskContext.Params中获取）
			for i := 1; i < fnType.NumIn(); i++ {
				paramType := fnType.In(i)
				paramValue := getParamFromTaskContext(taskCtx, i-1, paramType)

				// 转换类型
				convertedValue, err := convertParamToType(paramValue, paramType)
				if err != nil {
					stateCh <- JobFunctionState{
						Status: "Failed",
						Error:  fmt.Errorf("参数转换失败 [参数%d, 类型%v]: %w", i-1, paramType, err),
					}
					return
				}
				args[i] = convertedValue
			}

			// 调用函数
			results := fnValue.Call(args)

			// 处理返回值
			var result interface{}
			var err error

			if numOut == 1 {
				// 只返回error
				if results[0].IsNil() {
					err = nil
				} else {
					err = results[0].Interface().(error)
				}
			} else {
				// 返回(result, error)
				result = results[0].Interface()
				if !results[1].IsNil() {
					err = results[1].Interface().(error)
				}
			}

			if err != nil {
				stateCh <- JobFunctionState{
					Status: "Failed",
					Error:  err,
				}
				return
			}

			stateCh <- JobFunctionState{
				Status: "Success",
				Data:   result,
			}
		}()
		return stateCh
	}
}

// getParamFromTaskContext 从TaskContext中获取参数值
func getParamFromTaskContext(taskCtx *TaskContext, index int, paramType reflect.Type) interface{} {
	// 尝试多种key匹配方式
	keys := []string{
		fmt.Sprintf("arg%d", index),
		fmt.Sprintf("param%d", index+1),
		paramType.Name(),
		paramType.Kind().String(),
	}

	for _, key := range keys {
		if val := taskCtx.GetParam(key); val != nil {
			return val
		}
	}

	return nil
}

// convertParamToType 将参数值转换为指定类型
func convertParamToType(value interface{}, targetType reflect.Type) (reflect.Value, error) {
	if value == nil {
		return reflect.Zero(targetType), nil
	}

	valueType := reflect.TypeOf(value)
	if valueType.AssignableTo(targetType) {
		return reflect.ValueOf(value), nil
	}

	// 尝试类型转换
	if valueType.ConvertibleTo(targetType) {
		return reflect.ValueOf(value).Convert(targetType), nil
	}

	// 对于字符串，使用原有的convertStringToType逻辑
	if str, ok := value.(string); ok {
		return convertStringToType(str, targetType)
	}

	return reflect.Value{}, fmt.Errorf("无法将类型 %v 转换为 %v", valueType, targetType)
}

// convertStringToType 将字符串转换为指定类型
// 支持基本类型和通过JSON反序列化的复杂类型
func convertStringToType(value string, targetType reflect.Type) (reflect.Value, error) {
	// 首先检查是否是类型别名（如 type UserID string）
	// 类型别名：Kind是基本类型，但Name不是空字符串
	if targetType.Name() != "" && targetType.Kind() == reflect.String {
		// 这是string的类型别名，直接转换
		return reflect.ValueOf(value).Convert(targetType), nil
	}

	switch targetType.Kind() {
	case reflect.String:
		return reflect.ValueOf(value), nil
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		if value == "" {
			return reflect.Zero(targetType), nil
		}
		intVal, err := strconv.ParseInt(value, 10, 64)
		if err != nil {
			return reflect.Value{}, fmt.Errorf("无法转换为int: %w", err)
		}
		return reflect.ValueOf(intVal).Convert(targetType), nil
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		if value == "" {
			return reflect.Zero(targetType), nil
		}
		uintVal, err := strconv.ParseUint(value, 10, 64)
		if err != nil {
			return reflect.Value{}, fmt.Errorf("无法转换为uint: %w", err)
		}
		return reflect.ValueOf(uintVal).Convert(targetType), nil
	case reflect.Float32, reflect.Float64:
		if value == "" {
			return reflect.Zero(targetType), nil
		}
		floatVal, err := strconv.ParseFloat(value, 64)
		if err != nil {
			return reflect.Value{}, fmt.Errorf("无法转换为float: %w", err)
		}
		return reflect.ValueOf(floatVal).Convert(targetType), nil
	case reflect.Bool:
		if value == "" {
			return reflect.ValueOf(false), nil
		}
		boolVal, err := strconv.ParseBool(value)
		if err != nil {
			return reflect.Value{}, fmt.Errorf("无法转换为bool: %w", err)
		}
		return reflect.ValueOf(boolVal), nil
	case reflect.Struct:
		// 支持struct类型：通过JSON反序列化
		return convertStructFromJSON(value, targetType)
	case reflect.Slice:
		// 支持slice类型：通过JSON反序列化
		return convertSliceFromJSON(value, targetType)
	case reflect.Map:
		// 支持map类型：通过JSON反序列化
		return convertMapFromJSON(value, targetType)
	case reflect.Ptr:
		// 支持指针类型
		if targetType.Elem().Kind() == reflect.Struct {
			return convertPtrStructFromJSON(value, targetType)
		}
		// 其他指针类型尝试解引用后转换
		elemValue, err := convertStringToType(value, targetType.Elem())
		if err != nil {
			return reflect.Value{}, err
		}
		ptrValue := reflect.New(targetType.Elem())
		ptrValue.Elem().Set(elemValue)
		return ptrValue, nil
	default:
		// 尝试作为自定义类型别名处理（如 type UserID string）
		// 先尝试JSON反序列化
		if value != "" && (value[0] == '{' || value[0] == '[') {
			// 可能是JSON格式，尝试反序列化
			result := reflect.New(targetType).Interface()
			if err := json.Unmarshal([]byte(value), result); err == nil {
				return reflect.ValueOf(result).Elem(), nil
			}
		}
		// 尝试转换为底层类型
		return convertCustomTypeAlias(value, targetType)
	}
}

// convertStructFromJSON 从JSON字符串创建struct
func convertStructFromJSON(value string, targetType reflect.Type) (reflect.Value, error) {
	if value == "" {
		return reflect.Zero(targetType), nil
	}

	// 创建目标类型的指针
	result := reflect.New(targetType).Interface()

	// JSON反序列化
	if err := json.Unmarshal([]byte(value), result); err != nil {
		return reflect.Value{}, fmt.Errorf("JSON反序列化失败: %w", err)
	}

	// 返回解引用后的值
	return reflect.ValueOf(result).Elem(), nil
}

// convertSliceFromJSON 从JSON字符串创建slice
func convertSliceFromJSON(value string, targetType reflect.Type) (reflect.Value, error) {
	if value == "" {
		return reflect.MakeSlice(targetType, 0, 0), nil
	}

	// 创建目标类型的指针
	result := reflect.New(targetType).Interface()

	// JSON反序列化
	if err := json.Unmarshal([]byte(value), result); err != nil {
		return reflect.Value{}, fmt.Errorf("JSON反序列化失败: %w", err)
	}

	// 返回解引用后的值
	return reflect.ValueOf(result).Elem(), nil
}

// convertMapFromJSON 从JSON字符串创建map
func convertMapFromJSON(value string, targetType reflect.Type) (reflect.Value, error) {
	if value == "" {
		return reflect.MakeMap(targetType), nil
	}

	// 创建目标类型的指针
	result := reflect.New(targetType).Interface()

	// JSON反序列化
	if err := json.Unmarshal([]byte(value), result); err != nil {
		return reflect.Value{}, fmt.Errorf("JSON反序列化失败: %w", err)
	}

	// 返回解引用后的值
	return reflect.ValueOf(result).Elem(), nil
}

// convertPtrStructFromJSON 从JSON字符串创建指针struct
func convertPtrStructFromJSON(value string, targetType reflect.Type) (reflect.Value, error) {
	if value == "" {
		return reflect.Zero(targetType), nil
	}

	// 获取指针指向的类型
	elemType := targetType.Elem()

	// 创建目标类型的值
	result := reflect.New(elemType).Interface()

	// JSON反序列化
	if err := json.Unmarshal([]byte(value), result); err != nil {
		return reflect.Value{}, fmt.Errorf("JSON反序列化失败: %w", err)
	}

	// 返回指针值
	return reflect.ValueOf(result), nil
}

// convertCustomTypeAlias 处理自定义类型别名（如 type UserID string）
func convertCustomTypeAlias(value string, targetType reflect.Type) (reflect.Value, error) {
	// 对于类型别名，需要先转换为底层类型，然后再转换为目标类型
	// 例如：type UserID string，需要先转换为string，再转换为UserID

	// 检查是否是类型别名（Kind不是基本类型，但可以转换为基本类型）
	// 尝试直接创建目标类型的值
	targetValue := reflect.New(targetType).Interface()

	// 如果目标类型是string的别名，直接赋值
	if targetType.Kind() == reflect.String {
		// 直接使用字符串值
		return reflect.ValueOf(value).Convert(targetType), nil
	}

	// 尝试通过JSON反序列化（如果value是JSON格式）
	if value != "" && (value[0] == '{' || value[0] == '[' || value[0] == '"') {
		if err := json.Unmarshal([]byte(value), targetValue); err == nil {
			return reflect.ValueOf(targetValue).Elem(), nil
		}
	}

	// 如果都不行，尝试获取底层类型并转换
	// 对于类型别名，reflect.Type.Kind()会返回底层类型的Kind
	// 但我们需要通过Convert来转换
	underlyingKind := targetType.Kind()

	// 如果底层类型是string，直接转换
	if underlyingKind == reflect.String {
		return reflect.ValueOf(value).Convert(targetType), nil
	}

	// 尝试其他基本类型转换
	switch underlyingKind {
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		intVal, err := strconv.ParseInt(value, 10, 64)
		if err != nil {
			return reflect.Value{}, fmt.Errorf("无法转换为int类型别名: %w", err)
		}
		return reflect.ValueOf(intVal).Convert(targetType), nil
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		uintVal, err := strconv.ParseUint(value, 10, 64)
		if err != nil {
			return reflect.Value{}, fmt.Errorf("无法转换为uint类型别名: %w", err)
		}
		return reflect.ValueOf(uintVal).Convert(targetType), nil
	case reflect.Float32, reflect.Float64:
		floatVal, err := strconv.ParseFloat(value, 64)
		if err != nil {
			return reflect.Value{}, fmt.Errorf("无法转换为float类型别名: %w", err)
		}
		return reflect.ValueOf(floatVal).Convert(targetType), nil
	case reflect.Bool:
		boolVal, err := strconv.ParseBool(value)
		if err != nil {
			return reflect.Value{}, fmt.Errorf("无法转换为bool类型别名: %w", err)
		}
		return reflect.ValueOf(boolVal).Convert(targetType), nil
	}

	return reflect.Value{}, fmt.Errorf("不支持的自定义类型: %v", targetType)
}
