package v1alpha1

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestItem(t *testing.T) {
	testData := map[string]Type{
		"0":             Number,
		"3.141":         Number,
		"true":          Bool,
		"\"hello\"":     String,
		"{\"val\":123}": Map,
	}

	for data, expectedType := range testData {
		var itm Item
		err := json.Unmarshal([]byte(data), &itm)
		assert.Nil(t, err)
		assert.Equal(t, itm.Type, expectedType)
		jsonBytes, err := json.Marshal(itm)
		assert.Equal(t, string(data), string(jsonBytes))
	}
}
