package jsoniter

import (
	"errors"
	"io"
	"reflect"
	"sync/atomic"
	"unsafe"
)

type Config struct {
	IndentionStep                 int
	MarshalFloatWith6Digits       bool
	SupportUnexportedStructFields bool
	Tag                           string
}

type frozenConfig struct {
	indentionStep int
	decoderCache  unsafe.Pointer
	encoderCache  unsafe.Pointer
	extensions    []ExtensionFunc
	tag           string
}

var DEFAULT_CONFIG = Config{}.Froze()

func (cfg Config) Froze() *frozenConfig {
	frozenConfig := &frozenConfig{
		indentionStep: cfg.IndentionStep,
	}
	atomic.StorePointer(&frozenConfig.decoderCache, unsafe.Pointer(&map[string]Decoder{}))
	atomic.StorePointer(&frozenConfig.encoderCache, unsafe.Pointer(&map[string]Encoder{}))
	if cfg.MarshalFloatWith6Digits {
		frozenConfig.marshalFloatWith6Digits()
	}
	if cfg.SupportUnexportedStructFields {
		frozenConfig.supportUnexportedStructFields()
	}
	if cfg.Tag != "" {
		frozenConfig.tag = cfg.Tag
	} else {
		frozenConfig.tag = "json"
	}
	return frozenConfig
}

// RegisterExtension can register a custom extension
func (cfg *frozenConfig) RegisterExtension(extension ExtensionFunc) {
	cfg.extensions = append(cfg.extensions, extension)
}

func (cfg *frozenConfig) supportUnexportedStructFields() {
	cfg.RegisterExtension(func(type_ reflect.Type, field *reflect.StructField) ([]string, EncoderFunc, DecoderFunc) {
		return []string{field.Name}, nil, nil
	})
}

// EnableLossyFloatMarshalling keeps 10**(-6) precision
// for float variables for better performance.
func (cfg *frozenConfig) marshalFloatWith6Digits() {
	// for better performance
	cfg.addEncoderToCache(reflect.TypeOf((*float32)(nil)).Elem(), &funcEncoder{func(ptr unsafe.Pointer, stream *Stream) {
		val := *((*float32)(ptr))
		stream.WriteFloat32Lossy(val)
	}})
	cfg.addEncoderToCache(reflect.TypeOf((*float64)(nil)).Elem(), &funcEncoder{func(ptr unsafe.Pointer, stream *Stream) {
		val := *((*float64)(ptr))
		stream.WriteFloat64Lossy(val)
	}})
}

func (cfg *frozenConfig) addDecoderToCache(cacheKey reflect.Type, decoder Decoder) {
	done := false
	for !done {
		ptr := atomic.LoadPointer(&cfg.decoderCache)
		cache := *(*map[reflect.Type]Decoder)(ptr)
		copied := map[reflect.Type]Decoder{}
		for k, v := range cache {
			copied[k] = v
		}
		copied[cacheKey] = decoder
		done = atomic.CompareAndSwapPointer(&cfg.decoderCache, ptr, unsafe.Pointer(&copied))
	}
}

func (cfg *frozenConfig) addEncoderToCache(cacheKey reflect.Type, encoder Encoder) {
	done := false
	for !done {
		ptr := atomic.LoadPointer(&cfg.encoderCache)
		cache := *(*map[reflect.Type]Encoder)(ptr)
		copied := map[reflect.Type]Encoder{}
		for k, v := range cache {
			copied[k] = v
		}
		copied[cacheKey] = encoder
		done = atomic.CompareAndSwapPointer(&cfg.encoderCache, ptr, unsafe.Pointer(&copied))
	}
}

func (cfg *frozenConfig) getDecoderFromCache(cacheKey reflect.Type) Decoder {
	ptr := atomic.LoadPointer(&cfg.decoderCache)
	cache := *(*map[reflect.Type]Decoder)(ptr)
	return cache[cacheKey]
}

func (cfg *frozenConfig) getEncoderFromCache(cacheKey reflect.Type) Encoder {
	ptr := atomic.LoadPointer(&cfg.encoderCache)
	cache := *(*map[reflect.Type]Encoder)(ptr)
	return cache[cacheKey]
}

// CleanDecoders cleans decoders registered or cached
func (cfg *frozenConfig) CleanDecoders() {
	typeDecoders = map[string]Decoder{}
	fieldDecoders = map[string]Decoder{}
	atomic.StorePointer(&cfg.decoderCache, unsafe.Pointer(&map[string]Decoder{}))
}

// CleanEncoders cleans encoders registered or cached
func (cfg *frozenConfig) CleanEncoders() {
	typeEncoders = map[string]Encoder{}
	fieldEncoders = map[string]Encoder{}
	atomic.StorePointer(&cfg.encoderCache, unsafe.Pointer(&map[string]Encoder{}))
}

func (cfg *frozenConfig) MarshalToString(v interface{}) (string, error) {
	buf, err := cfg.Marshal(v)
	if err != nil {
		return "", err
	}
	return string(buf), nil
}

func (cfg *frozenConfig) Marshal(v interface{}) ([]byte, error) {
	stream := NewStream(cfg, nil, 256)
	stream.WriteVal(v)
	if stream.Error != nil {
		return nil, stream.Error
	}
	return stream.Buffer(), nil
}

func (cfg *frozenConfig) Unmarshal(data []byte, v interface{}) error {
	data = data[:lastNotSpacePos(data)]
	iter := ParseBytes(cfg, data)
	typ := reflect.TypeOf(v)
	if typ.Kind() != reflect.Ptr {
		// return non-pointer error
		return errors.New("the second param must be ptr type")
	}
	iter.ReadVal(v)
	if iter.head == iter.tail {
		iter.loadMore()
	}
	if iter.Error == io.EOF {
		return nil
	}
	if iter.Error == nil {
		iter.reportError("Unmarshal", "there are bytes left after unmarshal")
	}
	return iter.Error
}

func (cfg *frozenConfig) UnmarshalFromString(str string, v interface{}) error {
	data := []byte(str)
	data = data[:lastNotSpacePos(data)]
	iter := ParseBytes(cfg, data)
	iter.ReadVal(v)
	if iter.head == iter.tail {
		iter.loadMore()
	}
	if iter.Error == io.EOF {
		return nil
	}
	if iter.Error == nil {
		iter.reportError("UnmarshalFromString", "there are bytes left after unmarshal")
	}
	return iter.Error
}
