package pogs

import (
	"encoding/json"
	"fmt"

	"github.com/mitchellh/mapstructure"
	"github.com/pkg/errors"
)

// OriginConfigJSONHandler is a wrapper to serialize OriginConfig with type information, and deserialize JSON
// into an OriginConfig.
type OriginConfigJSONHandler struct {
	OriginConfig OriginConfig
}

func (ocjh *OriginConfigJSONHandler) MarshalJSON() ([]byte, error) {
	marshaler := make(map[string]OriginConfig, 1)
	marshaler[ocjh.OriginConfig.jsonType()] = ocjh.OriginConfig
	return json.Marshal(marshaler)
}

func (ocjh *OriginConfigJSONHandler) UnmarshalJSON(b []byte) error {
	var originJSON map[string]interface{}
	if err := json.Unmarshal(b, &originJSON); err != nil {
		return errors.Wrapf(err, "cannot unmarshal %s into originJSON", string(b))
	}

	if originConfig, ok := originJSON[httpType.String()]; ok {
		httpOriginConfig := &HTTPOriginConfig{}
		if err := mapstructure.Decode(originConfig, httpOriginConfig); err != nil {
			return errors.Wrapf(err, "cannot decode %+v into HTTPOriginConfig", originConfig)
		}
		ocjh.OriginConfig = httpOriginConfig
		return nil
	}

	if originConfig, ok := originJSON[wsType.String()]; ok {
		wsOriginConfig := &WebSocketOriginConfig{}
		if err := mapstructure.Decode(originConfig, wsOriginConfig); err != nil {
			return errors.Wrapf(err, "cannot decode %+v into WebSocketOriginConfig", originConfig)
		}
		ocjh.OriginConfig = wsOriginConfig
		return nil
	}

	if originConfig, ok := originJSON[helloWorldType.String()]; ok {
		helloWorldOriginConfig := &HelloWorldOriginConfig{}
		if err := mapstructure.Decode(originConfig, helloWorldOriginConfig); err != nil {
			return errors.Wrapf(err, "cannot decode %+v into HelloWorldOriginConfig", originConfig)
		}
		ocjh.OriginConfig = helloWorldOriginConfig
		return nil
	}

	return fmt.Errorf("cannot unmarshal %s into OriginConfig", string(b))
}

// FallibleConfigMarshaler is a wrapper for FallibleConfig to implement custom marshal logic
type FallibleConfigMarshaler struct {
	FallibleConfig FallibleConfig
}

func (fcm *FallibleConfigMarshaler) MarshalJSON() ([]byte, error) {
	marshaler := make(map[string]FallibleConfig, 1)
	marshaler[fcm.FallibleConfig.jsonType()] = fcm.FallibleConfig
	return json.Marshal(marshaler)
}
