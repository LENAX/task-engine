package unit

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/LENAX/task-engine/pkg/core/realtime"
)

func TestExtractFieldFromRawData_Map(t *testing.T) {
	// 常见 DataCollector 发布的 map 形态
	data := map[string]interface{}{
		"topic":  "HQ_STK_TICK",
		"code":   "600863.SH",
		"record": []interface{}{},
	}
	assert.Equal(t, "600863.SH", realtime.ExtractFieldFromRawData(data, "code"))
	assert.Equal(t, "HQ_STK_TICK", realtime.ExtractFieldFromRawData(data, "topic"))
	assert.Equal(t, "", realtime.ExtractFieldFromRawData(data, "symbol"))
	assert.Equal(t, "", realtime.ExtractFieldFromRawData(data, ""))
	assert.Equal(t, "", realtime.ExtractFieldFromRawData(nil, "code"))
}

func TestExtractFieldFromRawData_Map_NonStringIgnored(t *testing.T) {
	data := map[string]interface{}{
		"code": 123,
		"num":  "456",
	}
	assert.Equal(t, "", realtime.ExtractFieldFromRawData(data, "code")) // 数字不转
	assert.Equal(t, "456", realtime.ExtractFieldFromRawData(data, "num"))
}

func TestExtractFieldFromRawData_Struct_FieldName(t *testing.T) {
	type Payload struct {
		Code string
		Name string
	}
	p := Payload{Code: "600863.SH", Name: "华能"}
	assert.Equal(t, "600863.SH", realtime.ExtractFieldFromRawData(p, "code"))
	assert.Equal(t, "600863.SH", realtime.ExtractFieldFromRawData(p, "Code")) // 大小写不敏感
	assert.Equal(t, "华能", realtime.ExtractFieldFromRawData(p, "name"))
}

func TestExtractFieldFromRawData_Struct_JSONTag(t *testing.T) {
	type Payload struct {
		TsCode string `json:"ts_code"`
		Name   string `json:"name"`
	}
	p := Payload{TsCode: "600863.SH", Name: "华能"}
	assert.Equal(t, "600863.SH", realtime.ExtractFieldFromRawData(p, "ts_code"))
	assert.Equal(t, "华能", realtime.ExtractFieldFromRawData(p, "name"))
}

func TestExtractFieldFromRawData_Struct_Pointer(t *testing.T) {
	type Payload struct {
		Code string `json:"code"`
	}
	p := &Payload{Code: "000001.SZ"}
	assert.Equal(t, "000001.SZ", realtime.ExtractFieldFromRawData(p, "code"))
}

func TestExtractFieldFromRawData_Struct_InstrumentID(t *testing.T) {
	type OptPayload struct {
		InstrumentID string `json:"instrument_id"`
		TsCode      string `json:"ts_code"`
	}
	p := OptPayload{InstrumentID: "MO2503", TsCode: "510050.SH"}
	assert.Equal(t, "MO2503", realtime.ExtractFieldFromRawData(p, "instrument_id"))
	assert.Equal(t, "510050.SH", realtime.ExtractFieldFromRawData(p, "ts_code"))
}
