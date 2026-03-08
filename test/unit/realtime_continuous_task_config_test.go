package unit

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/LENAX/task-engine/pkg/core/realtime"
)

func TestNewContinuousTaskConfig_CollectorNameAndModeDefaults(t *testing.T) {
	cfg := realtime.NewContinuousTaskConfig("id1", "name1", realtime.TaskTypeDataCollector)
	assert.Equal(t, "", cfg.CollectorName)
	assert.Equal(t, realtime.CollectorModePush, cfg.Mode)
}

func TestNewContinuousTaskConfig_ExplicitCollectorNameAndMode(t *testing.T) {
	cfg := realtime.NewContinuousTaskConfig("id1", "name1", realtime.TaskTypeDataCollector)
	cfg.CollectorName = "my_collector"
	cfg.Mode = realtime.CollectorModePull
	assert.Equal(t, "my_collector", cfg.CollectorName)
	assert.Equal(t, realtime.CollectorModePull, cfg.Mode)
}

func TestCollectorMode_Constants(t *testing.T) {
	assert.Equal(t, "push", realtime.CollectorModePush)
	assert.Equal(t, "pull", realtime.CollectorModePull)
}
