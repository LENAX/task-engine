package workflow

import (
	"fmt"
	"strings"
)

// ReplacePlaceholder 替换单个占位符字符串
// value: 可能包含占位符的字符串
// params: 参数映射，key为占位符名称（不含${}），value为实际值
// 返回替换后的字符串和是否成功替换
func ReplacePlaceholder(value string, params map[string]interface{}) (string, bool) {
	if !strings.HasPrefix(value, "${") || !strings.HasSuffix(value, "}") {
		return value, false
	}

	// 提取占位符名称（去除${和}）
	paramName := strings.TrimPrefix(strings.TrimSuffix(value, "}"), "${")
	if paramName == "" {
		return value, false
	}

	// 从params中查找对应的值
	actualValue, exists := params[paramName]
	if !exists {
		return value, false
	}

	// 将实际值转换为字符串
	var strValue string
	switch v := actualValue.(type) {
	case string:
		strValue = v
	case nil:
		strValue = ""
	default:
		// 对于其他类型，使用fmt.Sprintf转换
		strValue = fmt.Sprintf("%v", v)
	}

	return strValue, true
}

// ReplaceParamsInMap 替换map中的参数占位符
// paramsMap: 需要替换的参数字典（sync.Map的简化版本，使用普通map）
// replacementParams: 参数映射，key为占位符名称（不含${}），value为实际值
// 返回未替换的占位符列表和错误
func ReplaceParamsInMap(paramsMap map[string]interface{}, replacementParams map[string]interface{}) ([]string, error) {
	var unreplaced []string

	for key, value := range paramsMap {
		// 只处理string类型的值
		strValue, ok := value.(string)
		if !ok {
			continue
		}

		// 尝试替换占位符
		replaced, success := ReplacePlaceholder(strValue, replacementParams)
		if success {
			paramsMap[key] = replaced
		} else if strings.HasPrefix(strValue, "${") && strings.HasSuffix(strValue, "}") {
			// 如果包含占位符格式但未找到对应的参数值，记录未替换的占位符
			paramName := strings.TrimPrefix(strings.TrimSuffix(strValue, "}"), "${")
			unreplaced = append(unreplaced, paramName)
		}
	}

	if len(unreplaced) > 0 {
		return unreplaced, fmt.Errorf("以下占位符未找到对应的参数值: %v", unreplaced)
	}

	return nil, nil
}
