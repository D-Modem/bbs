package p3

import (
	"encoding/binary"
	"fmt"
)

// Init is what the client says of itself in its INIT packet: the layout of
// the 32-bit Windows client (3.0 and on); other clients' INITs are shorter
// or laid out otherwise, and only what they have is filled in.
type Init struct {
	Platform      uint8 // 3 Windows, 0x0C Macintosh
	Version       uint8
	SubVersion    uint8
	MachineMemory uint8
	AppMemory     uint8
	PCType        uint16
	ReleaseMonth  uint8
	ReleaseDay    uint8
	CustomerClass uint16
	UDOTimestamp  uint32 // when its forms database was made
	DOSVersion    uint16
	SessionFlags  uint16
	VideoType     uint8
	Processor     uint8
	MediaType     uint32
	WindowsVer    uint32
	MemoryMode    uint8
	Width, Height uint16 // the screen's
	Colors        uint16
	Region        uint16
	Language      [4]uint16
	ConnectSpeed  uint8

	Raw []byte // the INIT's data as it came
}

// The platforms.
const (
	PlatformWindows = 0x03
	PlatformMac     = 0x0C
)

// ParseInit reads an INIT packet's data.
func ParseInit(d []byte) Init {
	in := Init{Raw: append([]byte(nil), d...)}
	b := make([]byte, 0x31)
	copy(b, d)
	be := binary.BigEndian
	in.Platform, in.Version, in.SubVersion = b[0], b[1], b[2]
	in.MachineMemory, in.AppMemory = b[4], b[5]
	in.PCType = be.Uint16(b[6:])
	in.ReleaseMonth, in.ReleaseDay = b[8], b[9]
	in.CustomerClass = be.Uint16(b[0x0A:])
	in.UDOTimestamp = be.Uint32(b[0x0C:])
	in.DOSVersion = be.Uint16(b[0x10:])
	in.SessionFlags = be.Uint16(b[0x12:])
	in.VideoType, in.Processor = b[0x14], b[0x15]
	in.MediaType = be.Uint32(b[0x16:])
	in.WindowsVer = be.Uint32(b[0x1A:])
	in.MemoryMode = b[0x1E]
	in.Width, in.Height = be.Uint16(b[0x1F:]), be.Uint16(b[0x21:])
	in.Colors = be.Uint16(b[0x23:])
	in.Region = be.Uint16(b[0x26:])
	for i := range in.Language {
		in.Language[i] = be.Uint16(b[0x28+2*i:])
	}
	in.ConnectSpeed = b[0x30]
	return in
}

func (in Init) String() string {
	p := fmt.Sprintf("platform %d", in.Platform)
	switch in.Platform {
	case PlatformWindows:
		p = "Windows"
	case PlatformMac:
		p = "Macintosh"
	}
	s := fmt.Sprintf("%s client, version %d/%d", p, in.Version, in.SubVersion)
	if in.Width != 0 {
		s += fmt.Sprintf(", %dx%d", in.Width, in.Height)
	}
	return s
}
