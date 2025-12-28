package main

import (
	"context"
	"encoding/json"
	"fmt"
	"reflect"
	"strconv"
	"sync"
	"time"

	"github.com/google/uuid"
)

// FunctionRegistry 函数注册中心
type FunctionRegistry struct {
	mu        sync.RWMutex
	functions map[string]TaskFunction  // 函数ID -> 函数实例
	metaMap   map[string]*FunctionMeta // 函数ID -> 元数据
	storage   *Storage
}

// NewFunctionRegistry 创建函数注册中心
func NewFunctionRegistry(storage *Storage) *FunctionRegistry {
	return &FunctionRegistry{
		functions: make(map[string]TaskFunction),
		metaMap:   make(map[string]*FunctionMeta),
		storage:   storage,
	}
}

// Register 注册函数
// fn: 用户自定义函数，第一个参数必须是context.Context，最后一个返回值必须是error
// name: 函数名称（唯一标识）
// description: 函数描述
func (r *FunctionRegistry) Register(ctx context.Context, name string, fn interface{}, description string) (string, error) {
	// 检查函数类型
	fnValue := reflect.ValueOf(fn)
	fnType := fnValue.Type()

	if fnType.Kind() != reflect.Func {
		return "", fmt.Errorf("参数必须是函数类型")
	}

	// 检查参数：第一个必须是context.Context
	if fnType.NumIn() < 1 {
		return "", fmt.Errorf("函数至少需要一个context.Context参数")
	}
	firstParamType := fnType.In(0)
	contextType := reflect.TypeOf((*context.Context)(nil)).Elem()
	if firstParamType != contextType {
		return "", fmt.Errorf("函数第一个参数必须是context.Context")
	}

	// 检查返回值：最后一个必须是error
	if fnType.NumOut() < 1 {
		return "", fmt.Errorf("函数必须至少返回一个error")
	}
	lastOutType := fnType.Out(fnType.NumOut() - 1)
	errorType := reflect.TypeOf((*error)(nil)).Elem()
	if !lastOutType.Implements(errorType) {
		return "", fmt.Errorf("函数最后一个返回值必须是error")
	}

	// 提取函数元数据
	meta, err := r.extractFunctionMeta(fn, name, description)
	if err != nil {
		return "", fmt.Errorf("提取函数元数据失败: %w", err)
	}

	// 生成函数ID
	meta.ID = uuid.NewString()
	meta.CreateTime = time.Now()

	// 包装函数为统一签名
	wrappedFunc, err := r.wrapFunction(fn)
	if err != nil {
		return "", fmt.Errorf("包装函数失败: %w", err)
	}

	// 保存到数据库
	if r.storage != nil {
		if err := r.storage.SaveFunction(ctx, meta); err != nil {
			return "", fmt.Errorf("保存函数元数据失败: %w", err)
		}
	}

	// 保存到内存
	r.mu.Lock()
	r.functions[meta.ID] = wrappedFunc
	r.metaMap[meta.ID] = meta
	r.mu.Unlock()

	return meta.ID, nil
}

// Get 根据函数ID获取函数
func (r *FunctionRegistry) Get(funcID string) TaskFunction {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.functions[funcID]
}

// GetMeta 根据函数ID获取元数据
func (r *FunctionRegistry) GetMeta(funcID string) *FunctionMeta {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.metaMap[funcID]
}

// LoadFromStorage 从数据库加载函数元数据（但不加载函数实例）
// 函数实例需要用户通过Register重新注册
func (r *FunctionRegistry) LoadFromStorage(ctx context.Context) ([]*FunctionMeta, error) {
	if r.storage == nil {
		return nil, fmt.Errorf("未配置存储")
	}

	metas, err := r.storage.ListAllFunctions(ctx)
	if err != nil {
		return nil, fmt.Errorf("从数据库加载函数元数据失败: %w", err)
	}

	// 将元数据加载到内存（但不加载函数实例）
	r.mu.Lock()
	for _, meta := range metas {
		r.metaMap[meta.ID] = meta
	}
	r.mu.Unlock()

	return metas, nil
}

// LoadFunction 从数据库加载函数元数据并注册函数实例
func (r *FunctionRegistry) LoadFunction(ctx context.Context, funcID string, fn interface{}) error {
	if r.storage == nil {
		return fmt.Errorf("未配置存储")
	}

	// 从数据库加载元数据
	meta, err := r.storage.GetFunctionByID(ctx, funcID)
	if err != nil {
		return fmt.Errorf("加载函数元数据失败: %w", err)
	}
	if meta == nil {
		return fmt.Errorf("函数ID %s 不存在", funcID)
	}

	// 包装函数
	wrappedFunc, err := r.wrapFunction(fn)
	if err != nil {
		return fmt.Errorf("包装函数失败: %w", err)
	}

	// 保存到内存
	r.mu.Lock()
	r.functions[funcID] = wrappedFunc
	r.metaMap[funcID] = meta
	r.mu.Unlock()

	return nil
}

// FunctionDef 函数定义，用于批量注册
type FunctionDef struct {
	Name        string      // 函数名称
	Description string      // 函数描述
	Function    interface{} // 函数实例
}

// RegisterBatch 批量注册函数
func (r *FunctionRegistry) RegisterBatch(ctx context.Context, functions []FunctionDef) error {
	for _, def := range functions {
		_, err := r.Register(ctx, def.Name, def.Function, def.Description)
		if err != nil {
			return fmt.Errorf("注册函数 %s 失败: %w", def.Name, err)
		}
	}
	return nil
}

// RestoreFunctions 从数据库恢复函数元数据，并通过函数映射表恢复函数实例
// funcMap: 函数名称 -> 函数实例的映射
func (r *FunctionRegistry) RestoreFunctions(ctx context.Context, funcMap map[string]interface{}) error {
	if r.storage == nil {
		return fmt.Errorf("未配置存储")
	}

	// 从数据库加载所有函数元数据
	metas, err := r.storage.ListAllFunctions(ctx)
	if err != nil {
		return fmt.Errorf("从数据库加载函数元数据失败: %w", err)
	}

	// 恢复函数实例
	for _, meta := range metas {
		fn, exists := funcMap[meta.Name]
		if !exists {
			// 函数实例不存在，只加载元数据
			r.mu.Lock()
			r.metaMap[meta.ID] = meta
			r.mu.Unlock()
			continue
		}

		// 加载函数实例
		if err := r.LoadFunction(ctx, meta.ID, fn); err != nil {
			return fmt.Errorf("恢复函数 %s 失败: %w", meta.Name, err)
		}
	}

	return nil
}

// GetByName 根据函数名称获取函数ID
func (r *FunctionRegistry) GetIDByName(name string) string {
	r.mu.RLock()
	defer r.mu.RUnlock()

	for id, meta := range r.metaMap {
		if meta.Name == name {
			return id
		}
	}
	return ""
}

// extractFunctionMeta 提取函数元数据
// 注意：不再提取参数类型和返回值类型，这些信息在运行时从函数实例通过反射获取
func (r *FunctionRegistry) extractFunctionMeta(fn interface{}, name, description string) (*FunctionMeta, error) {
	fnType := reflect.TypeOf(fn)
	if fnType.Kind() != reflect.Func {
		return nil, fmt.Errorf("参数必须是函数类型")
	}

	// 只验证函数签名，不提取参数类型信息
	// 参数类型信息在运行时通过反射从函数实例获取（在wrapFunction中）

	return &FunctionMeta{
		Name:        name,
		Description: description,
		// 不再存储 ParamTypes 和 ReturnType
		// 运行时从函数实例通过反射获取类型信息
	}, nil
}

// wrapFunction 将任意函数包装为TaskFunction统一签名，返回channel
func (r *FunctionRegistry) wrapFunction(fn interface{}) (TaskFunction, error) {
	fnValue := reflect.ValueOf(fn)
	fnType := fnValue.Type()

	// 构建包装函数，返回channel
	return func(ctx context.Context, params map[string]string) <-chan JobFunctionState {
		stateCh := make(chan JobFunctionState, 1)
		go func() {
			defer close(stateCh)

			// 准备函数参数
			args := make([]reflect.Value, fnType.NumIn())
			args[0] = reflect.ValueOf(ctx) // 第一个参数是context

			// 处理其余参数
			for i := 1; i < fnType.NumIn(); i++ {
				paramType := fnType.In(i)

				// 从params中获取值，支持多种匹配方式
				var paramValue string
				var found bool

				// 尝试使用索引作为key (arg0, arg1, ...)
				paramValue, found = params[fmt.Sprintf("arg%d", i-1)]
				if !found {
					// 尝试使用参数位置 (param1, param2, ...)
					paramValue, found = params[fmt.Sprintf("param%d", i)]
				}
				if !found {
					// 尝试使用类型名作为key
					typeName := paramType.Name()
					if typeName == "" {
						typeName = paramType.Kind().String()
					}
					paramValue, found = params[typeName]
				}

				// 如果仍然没找到，尝试使用类型名的小写形式
				if !found {
					typeName := paramType.Kind().String()
					paramValue, found = params[typeName]
				}

				// 如果还是没找到，使用空字符串（对于可选参数）
				if !found {
					paramValue = ""
				}

				// 转换类型
				convertedValue, err := convertStringToType(paramValue, paramType)
				if err != nil {
					stateCh <- JobFunctionState{
						Status: "Failed",
						Error:  fmt.Errorf("参数转换失败 [参数%d, 类型%v, 值'%s']: %w", i-1, paramType, paramValue, err),
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
			numOut := fnType.NumOut()

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
	}, nil
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
