// Package realtime 提供实时数据采集任务的实例管理器
package realtime

import (
	"reflect"
	"strings"
)

// ExtractFieldFromRawData 从 rawData 中解析指定字段的字符串值（支持 map 与 struct），用于订阅者过滤，兼容多种数据结构（如 TsStkBndFnd/TsIdx/TsOpt/TsMin 等）
func ExtractFieldFromRawData(rawData interface{}, field string) string {
	if rawData == nil || field == "" {
		return ""
	}
	// map[string]interface{}：常见于 DataCollector 发布的 JSON 解包结果
	if m, ok := rawData.(map[string]interface{}); ok {
		if v, ok := m[field]; ok {
			if s, ok := v.(string); ok {
				return s
			}
		}
		return ""
	}
	// struct：支持字段名（大小写不敏感）与 json tag，便于多种 payload 结构（code/TsCode/InstrumentID 等）
	v := reflect.ValueOf(rawData)
	if v.Kind() == reflect.Ptr {
		v = v.Elem()
	}
	if v.Kind() != reflect.Struct {
		return ""
	}
	t := v.Type()
	fieldLower := strings.ToLower(field)
	for i := 0; i < v.NumField(); i++ {
		sf := t.Field(i)
		name := sf.Name
		if jsonTag := sf.Tag.Get("json"); jsonTag != "" {
			if idx := strings.Index(jsonTag, ","); idx >= 0 {
				jsonTag = jsonTag[:idx]
			}
			if jsonTag != "" {
				name = jsonTag
			}
		}
		if strings.ToLower(name) != fieldLower {
			continue
		}
		fv := v.Field(i)
		if fv.Kind() == reflect.String {
			return fv.String()
		}
		return ""
	}
	return ""
}