package pbft

import (
	"github.com/stretchr/testify/assert"
	"testing"
)

func Test_loadConfig(t *testing.T) {
	assert := assert.New(t)
	err := loadConfig()
	assert.Nil(err)
}
