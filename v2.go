package proxyproto

import (
	"bufio"
	"bytes"
	"encoding/binary"
	"io"
	"net"
	"strconv"
)

var (
	lengthV4Bytes = func() []byte {
		a := make([]byte, 2)
		binary.BigEndian.PutUint16(a, 12)
		return a
	}()
	lengthV6Bytes = func() []byte {
		a := make([]byte, 2)
		binary.BigEndian.PutUint16(a, 36)
		return a
	}()
	lengthUnixBytes = func() []byte {
		a := make([]byte, 2)
		binary.BigEndian.PutUint16(a, 218)
		return a
	}()
)

type _ports struct {
	SrcPort uint16
	DstPort uint16
}

type _addr4 struct {
	Src     [4]byte
	Dst     [4]byte
	SrcPort uint16
	DstPort uint16
}

type _addr6 struct {
	Src [16]byte
	Dst [16]byte
	_ports
}

type _addrUnix struct {
	Src [108]byte
	Dst [108]byte
}

func parseVersion2(reader *bufio.Reader) (header *Header, err error) {
	// Skip first 12 bytes (signature)
	for i := 0; i < 12; i++ {
		if _, err = reader.ReadByte(); err != nil {
			return nil, ErrCantReadProtocolVersionAndCommand
		}
	}

	header = new(Header)
	header.Version = 2

	// Read the 13th byte, protocol version and command
	b13, err := reader.ReadByte()
	if err != nil {
		return nil, ErrCantReadProtocolVersionAndCommand
	}
	header.Command = ProtocolVersionAndCommand(b13)
	if _, ok := supportedCommand[header.Command]; !ok {
		return nil, ErrUnsupportedProtocolVersionAndCommand
	}
	// If command is LOCAL, header ends here
	if header.Command.IsLocal() {
		return header, nil
	}

	// Read the 14th byte, address family and protocol
	b14, err := reader.ReadByte()
	if err != nil {
		return nil, ErrCantReadAddressFamilyAndProtocol
	}
	header.TransportProtocol = AddressFamilyAndProtocol(b14)
	if _, ok := supportedTransportProtocol[header.TransportProtocol]; !ok {
		return nil, ErrUnsupportedAddressFamilyAndProtocol
	}

	// Read addresses and ports
	var length uint16
	if err := binary.Read(io.LimitReader(reader, 2), binary.BigEndian, &length); err != nil {
		return nil, ErrCantReadLength
	}
	if !header.validateLength(length) {
		return nil, ErrInvalidLength
	}

	if header.TransportProtocol.IsIPv4() {
		var addr _addr4
		if err := binary.Read(io.LimitReader(reader, int64(length)), binary.BigEndian, &addr); err != nil {
			return nil, ErrInvalidAddress
		}
		header.SourceAddress = &net.IPAddr{IP: addr.Src[:], Zone: ""}
		header.DestinationAddress = &net.IPAddr{IP: addr.Dst[:], Zone: ""}
		header.SourcePort = addr.SrcPort
		header.DestinationPort = addr.DstPort
	} else if header.TransportProtocol.IsIPv6() {
		var addr _addr6
		if err := binary.Read(io.LimitReader(reader, int64(length)), binary.BigEndian, &addr); err != nil {
			return nil, ErrInvalidAddress
		}
		header.SourceAddress = &net.IPAddr{IP: addr.Src[:], Zone: ""}
		header.DestinationAddress = &net.IPAddr{IP: addr.Dst[:], Zone: ""}
		header.SourcePort = addr.SrcPort
		header.DestinationPort = addr.DstPort
	} else if header.TransportProtocol.IsUnix() {
		var addr _addrUnix
		if err := binary.Read(io.LimitReader(reader, int64(length)), binary.BigEndian, &addr); err != nil {
			return nil, ErrInvalidAddress
		}
		if header.SourceAddress, err = net.ResolveUnixAddr("unix", string(addr.Src[:])); err != nil {
			return nil, ErrCantResolveSourceUnixAddress
		}
		if header.DestinationAddress, err = net.ResolveUnixAddr("unix", string(addr.Dst[:])); err != nil {
			return nil, ErrCantResolveDestinationUnixAddress
		}
	}

	// TODO add encapsulated TLV support

	return header, nil
}

func (header *Header) writeVersion2(w io.Writer) (int64, error) {
	var buf bytes.Buffer
	buf.Write(SIGV2)
	buf.WriteByte(header.Command.toByte())
	buf.WriteByte(header.TransportProtocol.toByte())
	// TODO add encapsulated TLV length
	var addrSrc, addrDst []byte
	if header.TransportProtocol.IsIPv4() {
		buf.Write(lengthV4Bytes)
		src, _ := net.ResolveIPAddr(INET4, header.SourceAddress.String())
		addrSrc = src.IP.To4()
		dst, _ := net.ResolveIPAddr(INET4, header.DestinationAddress.String())
		addrDst = dst.IP.To4()
	} else if header.TransportProtocol.IsIPv6() {
		buf.Write(lengthV6Bytes)
		src, _ := net.ResolveIPAddr(INET6, header.SourceAddress.String())
		addrSrc = src.IP.To16()
		dst, _ := net.ResolveIPAddr(INET6, header.DestinationAddress.String())
		addrDst = dst.IP.To16()
	} else if header.TransportProtocol.IsUnix() {
		buf.Write(lengthUnixBytes)
		// TODO is below right?
		addrSrc = []byte(header.SourceAddress.String())
		addrDst = []byte(header.DestinationAddress.String())
	}
	buf.Write(addrSrc)
	buf.Write(addrDst)
	buf.WriteString(strconv.Itoa(int(header.SourcePort)))
	buf.WriteString(strconv.Itoa(int(header.DestinationPort)))

	return buf.WriteTo(w)
}

func (header *Header) validateLength(length uint16) bool {
	if header.TransportProtocol.IsIPv4() {
		return length == 12
	} else if header.TransportProtocol.IsIPv6() {
		return length == 36
	} else if header.TransportProtocol.IsUnix() {
		return length == 218
	}
	return false
}
