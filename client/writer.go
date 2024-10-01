package client

import (
	"bufio"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"github.com/asticode/go-astiav"
	"github.com/pion/webrtc/v3"
	"github.com/pion/webrtc/v3/pkg/media"
)

// nolint: gochecknoglobals
var (
	inputFormatContext *astiav.FormatContext

	decodeCodecContext *astiav.CodecContext
	decodePacket       *astiav.Packet
	decodeFrame        *astiav.Frame
	videoStream        *astiav.Stream

	softwareScaleContext *astiav.SoftwareScaleContext
	scaledFrame          *astiav.Frame
	encodeCodecContext   *astiav.CodecContext
	encodePacket         *astiav.Packet

	pts int64
	err error
)

const h264FrameDuration = time.Millisecond * 20

func writeH264ToTrack(track *webrtc.TrackLocalStaticSample) {
	astiav.RegisterAllDevices()

	initTestSrc()
	defer freeVideoCoding()

	ticker := time.NewTicker(h264FrameDuration)
	for ; true; <-ticker.C {
		// Read frame from lavfi
		if err = inputFormatContext.ReadFrame(decodePacket); err != nil {
			if errors.Is(err, astiav.ErrEof) {
				break
			}
			panic(err)
		}

		decodePacket.RescaleTs(videoStream.TimeBase(), decodeCodecContext.TimeBase())

		// Send the packet
		if err = decodeCodecContext.SendPacket(decodePacket); err != nil {
			panic(err)
		}

		for {
			// Read Decoded Frame
			if err = decodeCodecContext.ReceiveFrame(decodeFrame); err != nil {
				if errors.Is(err, astiav.ErrEof) || errors.Is(err, astiav.ErrEagain) {
					break
				}
				panic(err)
			}

			// Init the Scaling+Encoding. Can't be started until we know info on input video
			initVideoEncoding()

			// Scale the video
			if err = softwareScaleContext.ScaleFrame(decodeFrame, scaledFrame); err != nil {
				panic(err)
			}

			// We don't care about the PTS, but encoder complains if unset
			pts++
			scaledFrame.SetPts(pts)

			// Encode the frame
			if err = encodeCodecContext.SendFrame(scaledFrame); err != nil {
				panic(err)
			}

			for {
				// Read encoded packets and write to file
				encodePacket = astiav.AllocPacket()
				if err = encodeCodecContext.ReceivePacket(encodePacket); err != nil {
					if errors.Is(err, astiav.ErrEof) || errors.Is(err, astiav.ErrEagain) {
						break
					}
					panic(err)
				}

				// Write H264 to track
				if err = track.WriteSample(media.Sample{Data: encodePacket.Data(), Duration: h264FrameDuration}); err != nil {
					panic(err)
				}
			}
		}
	}
}

func initTestSrc() {
	if inputFormatContext = astiav.AllocFormatContext(); inputFormatContext == nil {
		panic("Failed to AllocCodecContext")
	}

	// Open input
	if err = inputFormatContext.OpenInput("udp://224.0.0.251:5353", nil, nil); err != nil {
		panic(err)
	}

	// Find stream info
	if err = inputFormatContext.FindStreamInfo(nil); err != nil {
		panic(err)
	}

	videoStream = inputFormatContext.Streams()[0]

	decodeCodec := astiav.FindDecoder(videoStream.CodecParameters().CodecID())
	if decodeCodec == nil {
		panic("FindDecoder returned nil")
	}

	if decodeCodecContext = astiav.AllocCodecContext(decodeCodec); decodeCodecContext == nil {
		panic(err)
	}

	if err = videoStream.CodecParameters().ToCodecContext(decodeCodecContext); err != nil {
		panic(err)
	}

	decodeCodecContext.SetFramerate(inputFormatContext.GuessFrameRate(videoStream, nil))

	if err = decodeCodecContext.Open(decodeCodec, nil); err != nil {
		panic(err)
	}

	decodePacket = astiav.AllocPacket()
	decodeFrame = astiav.AllocFrame()
}

func initVideoEncoding() {
	if encodeCodecContext != nil {
		return
	}

	h264Encoder := astiav.FindEncoder(astiav.CodecIDH264)
	if h264Encoder == nil {
		panic("No H264 Encoder Found")
	}

	if encodeCodecContext = astiav.AllocCodecContext(h264Encoder); encodeCodecContext == nil {
		panic("Failed to AllocCodecContext Decoder")
	}

	encodeCodecContext.SetPixelFormat(astiav.PixelFormatYuv420P)
	encodeCodecContext.SetSampleAspectRatio(decodeCodecContext.SampleAspectRatio())
	encodeCodecContext.SetTimeBase(astiav.NewRational(1, 30))
	encodeCodecContext.SetWidth(decodeCodecContext.Width())
	encodeCodecContext.SetHeight(decodeCodecContext.Height())

	if err = encodeCodecContext.Open(h264Encoder, nil); err != nil {
		panic(err)
	}

	softwareScaleContext, err = astiav.CreateSoftwareScaleContext(
		decodeCodecContext.Width(),
		decodeCodecContext.Height(),
		decodeCodecContext.PixelFormat(),
		decodeCodecContext.Width(),
		decodeCodecContext.Height(),
		astiav.PixelFormatYuv420P,
		astiav.NewSoftwareScaleContextFlags(astiav.SoftwareScaleContextFlagBilinear),
	)
	if err != nil {
		panic(err)
	}

	scaledFrame = astiav.AllocFrame()
}

func freeVideoCoding() {
	inputFormatContext.CloseInput()
	inputFormatContext.Free()

	decodeCodecContext.Free()
	decodePacket.Free()
	decodeFrame.Free()

	scaledFrame.Free()
	softwareScaleContext.Free()
	encodeCodecContext.Free()
	encodePacket.Free()
}

// Read from stdin until we get a newline
func readUntilNewline() (in string) {
	var err error

	r := bufio.NewReader(os.Stdin)
	for {
		in, err = r.ReadString('\n')
		if err != nil && !errors.Is(err, io.EOF) {
			panic(err)
		}

		if in = strings.TrimSpace(in); len(in) > 0 {
			break
		}
	}

	fmt.Println("")
	return
}

// JSON encode + base64 a SessionDescription
func encode(obj *webrtc.SessionDescription) string {
	b, err := json.Marshal(obj)
	if err != nil {
		panic(err)
	}

	return base64.StdEncoding.EncodeToString(b)
}

// Decode a base64 and unmarshal JSON into a SessionDescription
func decode(in string, obj *webrtc.SessionDescription) {
	b, err := base64.StdEncoding.DecodeString(in)
	if err != nil {
		panic(err)
	}

	if err = json.Unmarshal(b, obj); err != nil {
		panic(err)
	}
}