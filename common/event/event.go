package event

import (
	"bytes"
	"crypto/rc4"
	"encoding/binary"
	"errors"
	_ "fmt"
	"reflect"

	"github.com/golang/snappy"
)

const (
	MAGIC_NUMBER uint16 = 0xCAFE
)

var EBNR = errors.New("Event buffer not ready")

var rc4Key string

type EventFlags uint64

func (f EventFlags) IsSnappyEnable() bool {
	return f&SnappyCompressor > 0
}
func (f EventFlags) IsRC4Enable() bool {
	return ((f >> 4) & RC4Encypter) > 0
}
func (f *EventFlags) EnableSnappy() {
	(*f) = EventFlags(uint64(*f) | SnappyCompressor)
}

func (f *EventFlags) EnableRC4() {
	(*f) = EventFlags(uint64(*f) | (RC4Encypter << 4))
}

func SetDefaultRC4Key(key string) {
	rc4Key = key
}

func EncodeInt64Value(buf *bytes.Buffer, v int64) {
	b := make([]byte, 10)
	size := binary.PutVarint(b, v)
	buf.Write(b[:size])
}

func DecodeInt64Value(buf *bytes.Buffer) (int64, error) {
	return binary.ReadVarint(buf)
}

func EncodeUInt32Value(buf *bytes.Buffer, v uint32) {
	b := make([]byte, 10)
	size := binary.PutUvarint(b, uint64(v))
	buf.Write(b[:size])
}

func EncodeUInt64Value(buf *bytes.Buffer, v uint64) {
	b := make([]byte, 10)
	size := binary.PutUvarint(b, v)
	buf.Write(b[:size])
}

func DecodeUInt64Value(buf *bytes.Buffer) (uint64, error) {
	return binary.ReadUvarint(buf)
}

func DecodeUInt16Value(buf *bytes.Buffer) (uint16, error) {
	tmp, err := binary.ReadUvarint(buf)
	return uint16(tmp), err
}

func DecodeUInt32Value(buf *bytes.Buffer) (uint32, error) {
	tmp, err := binary.ReadUvarint(buf)
	return uint32(tmp), err
}

func DecodeInt32Value(buf *bytes.Buffer) (int32, error) {
	tmp, err := binary.ReadVarint(buf)
	return int32(tmp), err
}

func EncodeBoolValue(buf *bytes.Buffer, v bool) {
	if v {
		EncodeUInt64Value(buf, 1)
	} else {
		EncodeUInt64Value(buf, 0)
	}
}

func DecodeBoolValue(buf *bytes.Buffer) (v bool, err error) {
	var b byte
	if b, err = buf.ReadByte(); nil != err {
		return
	} else {
		if b == 0 {
			v = false
		} else {
			v = true
		}
	}
	return
}

func EncodeBytesValue(buf *bytes.Buffer, v []byte) {
	if nil == v {
		EncodeUInt64Value(buf, 0)
	} else {
		EncodeUInt64Value(buf, uint64(len(v)))
		buf.Write(v)
	}
}

func EncodeByteBufferValue(buf *bytes.Buffer, v *bytes.Buffer) {
	if nil == v {
		EncodeUInt64Value(buf, 0)
	} else {
		EncodeUInt64Value(buf, uint64(v.Len()))
		buf.ReadFrom(v)
		//buf.Write(v)
	}
}

func EncodeStringValue(buf *bytes.Buffer, v string) {
	EncodeUInt64Value(buf, uint64(len(v)))
	buf.Write([]byte(v))
}

func DecodeBytesValue(buf *bytes.Buffer) (b []byte, err error) {
	var size uint64
	if size, err = binary.ReadUvarint(buf); nil != err {
		return
	}
	b = make([]byte, size)
	buf.Read(b)
	return
}

func DecodeByteBufferValue(buf *bytes.Buffer, dst *bytes.Buffer) (err error) {
	var size uint64
	if size, err = binary.ReadUvarint(buf); nil != err {
		return
	}
	if buf.Len() < int(size) {
		return errors.New("No sufficient space.")
	}
	dst.Write(buf.Next(int(size)))
	return nil
}

func DecodeStringValue(buf *bytes.Buffer) (str string, err error) {
	var size uint64
	if size, err = binary.ReadUvarint(buf); nil != err {
		return
	}
	b := make([]byte, size)
	buf.Read(b)
	str = string(b)
	return
}

func encodeValue(buf *bytes.Buffer, v *reflect.Value) {
	switch v.Type().Kind() {
	case reflect.Bool:
		EncodeBoolValue(buf, v.Bool())
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		EncodeInt64Value(buf, v.Int())
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		EncodeUInt64Value(buf, v.Uint())
	case reflect.String:
		EncodeBytesValue(buf, []byte(v.String()))
	case reflect.Array, reflect.Slice:
		if v.Type().Kind() == reflect.Slice && v.IsNil() {
			EncodeUInt64Value(buf, 0)
		} else {
			EncodeUInt64Value(buf, uint64(v.Len()))
			for i := 0; i < v.Len(); i++ {
				iv := v.Index(i)
				encodeValue(buf, &iv)
			}
		}
	case reflect.Map:
		if v.IsNil() {
			EncodeUInt64Value(buf, 0)
		} else {
			ks := v.MapKeys()
			EncodeUInt64Value(buf, uint64(len(ks)))
			for i := 0; i < len(ks); i++ {
				encodeValue(buf, &(ks[i]))
				vv := v.MapIndex(ks[i])
				encodeValue(buf, &vv)
			}
		}
	case reflect.Ptr:
		rv := reflect.Indirect(*v)
		encodeValue(buf, &rv)
	case reflect.Interface:
		rv := reflect.ValueOf(v.Interface())
		encodeValue(buf, &rv)
	case reflect.Struct:
		if m, exist := reflect.PtrTo(v.Type()).MethodByName("Encode"); exist {
			m.Func.Call([]reflect.Value{v.Addr(), reflect.ValueOf(buf)})
		} else {
			num := v.NumField()
			for i := 0; i < num; i++ {
				f := v.Field(i)
				encodeValue(buf, &f)
			}
		}
	}
}

func decodeValue(buf *bytes.Buffer, v *reflect.Value) error {
	switch v.Type().Kind() {
	case reflect.Bool:
		b, err := DecodeBoolValue(buf)
		if nil == err {
			v.SetBool(b)
		} else {
			return err
		}
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		b, err := binary.ReadVarint(buf)
		if nil == err {
			v.SetInt(b)
		} else {
			return err
		}
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		b, err := binary.ReadUvarint(buf)
		if nil == err {
			v.SetUint(b)
		} else {
			return err
		}
	case reflect.String:
		b, err := DecodeBytesValue(buf)
		if nil == err {
			v.SetString(string(b))
		} else {
			return err
		}
	case reflect.Array, reflect.Slice:
		b, err := binary.ReadUvarint(buf)
		if nil == err {
			sv := reflect.MakeSlice(v.Type(), int(b), int(b))
			for i := 0; i < int(b); i++ {
				iv := sv.Index(i)
				decodeValue(buf, &(iv))
			}
			v.Set(sv)
		} else {
			return err
		}
	case reflect.Map:
		b, err := binary.ReadUvarint(buf)
		if nil == err {
			vm := reflect.MakeMap(v.Type())
			for i := 0; i < int(b); i++ {
				kv := reflect.New(v.Type().Key()).Elem()
				vv := reflect.New(v.Type().Elem()).Elem()
				err := decodeValue(buf, &(kv))
				if nil != err {
					return err
				}
				err = decodeValue(buf, &(vv))
				if nil != err {
					return err
				}
				vm.SetMapIndex(kv, vv)
			}
			v.Set(vm)
		} else {
			return err
		}
	case reflect.Ptr:
		var err error
		if v.IsNil() {
			pv := reflect.New(v.Type().Elem())
			err = decodeValue(buf, &pv)
			v.Set(pv)
		} else {
			xv := v.Elem()
			err = decodeValue(buf, &xv)
		}
		if nil != err {
			return err
		} else {
			//v.Set(pv.Addr())
		}
	case reflect.Interface:
		rv := v.Elem()
		if rv.Type().Kind() == reflect.Interface {
			return errors.New("Loop interface:")
		}
		if !rv.CanSet() {
			return errors.New("Can not set interface")
		}
		err := decodeValue(buf, &rv)
		if nil != err {
			return err
		}
	case reflect.Struct:
		if m, exist := reflect.PtrTo(v.Type()).MethodByName("Decode"); exist {
			ret := m.Func.Call([]reflect.Value{v.Addr(), reflect.ValueOf(buf)})
			if ret[0].IsNil() {
				return nil
			}
			return ret[0].Interface().(error)
		} else {
			num := v.NumField()
			if !v.CanSet() {
				return errors.New("struct not settable")
			}
			for i := 0; i < num; i++ {
				f := v.Field(i)
				if !f.CanSet() {
					return errors.New("Field not settable")
				}
				err := decodeValue(buf, &f)
				if nil != err {
					return err
				}
			}
		}

	default:
		return errors.New("Unsupported type:" + v.Type().Name())
	}
	return nil
}

type EventHeader struct {
	Type  uint16
	Id    uint32
	Flags EventFlags
}

func (h *EventHeader) GetType() uint16 {
	return h.Type
}
func (h *EventHeader) GetFlags() EventFlags {
	return h.Flags
}

func (h *EventHeader) GetId() uint32 {
	return h.Id
}
func (h *EventHeader) SetId(hash uint32) {
	h.Id = hash
}

func (header *EventHeader) Encode(buffer *bytes.Buffer) {
	EncodeUInt64Value(buffer, uint64(header.Type))
	EncodeUInt64Value(buffer, uint64(header.Id))
	EncodeUInt64Value(buffer, uint64(header.Flags))
}
func (header *EventHeader) Decode(buffer *bytes.Buffer) error {
	var err error
	header.Type, err = DecodeUInt16Value(buffer)
	if nil != err {
		return err
	}
	header.Id, err = DecodeUInt32Value(buffer)
	if nil != err {
		return err
	}
	var f uint64
	f, err = DecodeUInt64Value(buffer)
	if nil != err {
		return err
	}
	header.Flags = EventFlags(f)
	return nil
}

type Event interface {
	Encode(buffer *bytes.Buffer)
	Decode(buffer *bytes.Buffer) error
	GetId() uint32
	SetId(id uint32)
}

func EncodeValue(buf *bytes.Buffer, ev interface{}) error {
	if tk := GetRegistType(ev); 0 == tk {
		return errors.New("No regist info to encode value.")
	} else {
		EncodeUInt64Value(buf, uint64(tk))
		rv := reflect.ValueOf(ev)
		encodeValue(buf, &rv)
	}
	return nil
}

func DecodeValue(buf *bytes.Buffer) (err error, ev interface{}) {
	t, err := DecodeUInt16Value(buf)
	if nil != err {
		return
	}
	err, ev = NewObjectInstance(t)
	rv := reflect.ValueOf(ev)
	err = decodeValue(buf, &rv)
	return
}

func EncodeEvent(buf *bytes.Buffer, ev Event) error {
	buf.Write(make([]byte, 4))
	var header EventHeader
	header.Type = GetRegistType(ev)
	header.Id = ev.GetId()
	header.Flags = 0
	if len(rc4Key) > 0 {
		header.Flags.EnableRC4()
	}
	var eventBuffer bytes.Buffer
	ev.Encode(&eventBuffer)
	eventContent := eventBuffer.Bytes()
	if eventBuffer.Len() >= 12 {
		header.Flags.EnableSnappy()
	}
	header.Encode(buf)
	if header.Flags.IsSnappyEnable() {
		evbuf := make([]byte, 0)
		newbuf := snappy.Encode(evbuf, eventContent)
		eventContent = newbuf
	}
	if header.Flags.IsRC4Enable() {
		EncodeUInt64Value(buf, uint64(len(eventContent)))
		dst := make([]byte, len(eventContent))
		cipher, _ := rc4.NewCipher([]byte(rc4Key))
		cipher.XORKeyStream(dst, eventContent)
		buf.Write(dst)
	} else {
		EncodeUInt64Value(buf, uint64(len(eventContent)))
		buf.Write(eventContent)
	}
	elen := uint32(buf.Len())
	binary.LittleEndian.PutUint32(buf.Bytes()[0:4], elen)
	return nil
}

func DecodeEvent(buf *bytes.Buffer) (err error, ev Event) {
	if buf.Len() < 4 {
		return EBNR, nil
	}
	elen := binary.LittleEndian.Uint32(buf.Bytes()[0:4])
	if elen > uint32(buf.Len()) {
		return EBNR, nil
	}
	buf.Next(4)
	var header EventHeader
	if err = header.Decode(buf); nil != err {
		return
	}
	var length uint32
	length, err = DecodeUInt32Value(buf)
	if err != nil {
		return
	}
	body := buf.Next(int(length))
	if header.Flags.IsRC4Enable() {
		dst := make([]byte, int(length))
		cipher, _ := rc4.NewCipher([]byte(rc4Key))
		cipher.XORKeyStream(dst, body)
		body = dst
	}
	if header.Flags.IsSnappyEnable() {
		b := make([]byte, 0, 0)
		b, err = snappy.Decode(b, body)
		if err != nil {
			return
		}
		body = b
	}
	var tmp interface{}
	if err, tmp = NewEventInstance(header.Type); nil != err {
		return
	}
	ev = tmp.(Event)
	ev.SetId(header.Id)
	err = ev.Decode(bytes.NewBuffer(body))
	return
}
