package backfill

import (
	"encoding/binary"
	"math"
)

// Protobuf wire types.
const (
	wireVarint  = 0
	wireFixed64 = 1
	wireLenDel  = 2
)

// encodeWriteRequest serialises a Prometheus Remote Write WriteRequest.
// Proto: message WriteRequest { repeated TimeSeries timeseries = 1; }
func encodeWriteRequest(series []TimeSeries) []byte {
	var buf []byte
	for _, ts := range series {
		data := encodeTimeSeries(ts)
		buf = appendLenDelim(buf, 1, data)
	}
	return buf
}

// encodeTimeSeries serialises one TimeSeries.
// Proto: message TimeSeries { repeated Label labels = 1; repeated Sample samples = 2; }
func encodeTimeSeries(ts TimeSeries) []byte {
	var buf []byte
	for _, l := range ts.Labels {
		buf = appendLenDelim(buf, 1, encodeLabel(l))
	}
	for _, s := range ts.Samples {
		buf = appendLenDelim(buf, 2, encodeSample(s))
	}
	return buf
}

// encodeLabel serialises one Label.
// Proto: message Label { string name = 1; string value = 2; }
func encodeLabel(l Label) []byte {
	var buf []byte
	buf = appendString(buf, 1, l.Name)
	buf = appendString(buf, 2, l.Value)
	return buf
}

// encodeSample serialises one Sample.
// Proto: message Sample { double value = 1; int64 timestamp = 2; }
func encodeSample(s Sample) []byte {
	var buf []byte
	// field 1: double (wire type 1 = fixed64)
	buf = appendTag(buf, 1, wireFixed64)
	buf = binary.LittleEndian.AppendUint64(buf, math.Float64bits(s.Value))
	// field 2: int64 (wire type 0 = varint)
	buf = appendTag(buf, 2, wireVarint)
	buf = binary.AppendVarint(buf, s.TimestampMs)
	return buf
}

// appendTag appends a protobuf field tag.
func appendTag(buf []byte, fieldNum int, wireType int) []byte {
	return binary.AppendUvarint(buf, uint64(fieldNum<<3|wireType))
}

// appendLenDelim appends a length-delimited field (tag + varint(len) + data).
func appendLenDelim(buf []byte, fieldNum int, data []byte) []byte {
	buf = appendTag(buf, fieldNum, wireLenDel)
	buf = binary.AppendUvarint(buf, uint64(len(data)))
	buf = append(buf, data...)
	return buf
}

// appendString appends a string field (same as length-delimited).
func appendString(buf []byte, fieldNum int, s string) []byte {
	buf = appendTag(buf, fieldNum, wireLenDel)
	buf = binary.AppendUvarint(buf, uint64(len(s)))
	buf = append(buf, s...)
	return buf
}
