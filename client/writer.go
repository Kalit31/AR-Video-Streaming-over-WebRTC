package client

import (
	"errors"
	"fmt"
	"strconv"
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

	encodeCodecContext   *astiav.CodecContext
	encodePacket         *astiav.Packet

	filterFrame       *astiav.Frame
	filterGraph       *astiav.FilterGraph
	brightnessFilter  *astiav.FilterContext
	buffersinkCtx    *astiav.FilterContext
	buffersrcCtx     *astiav.FilterContext

	pts int64
	err error
)

const h264FrameDuration = time.Millisecond * 20

func writeH264ToTrack(track *webrtc.TrackLocalStaticSample) {
	/*
	This function continuously reads video frames from a specified input, decodes them, 
	scales them, encodes them back into H.264 format, and writes the samples to a WebRTC track.
	*/
	astiav.RegisterAllDevices()

	initTestSrc()
	initFilters() 
	defer freeVideoCoding()

	ticker := time.NewTicker(h264FrameDuration)
	for ; true; <-ticker.C {
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

			initVideoEncoding()

			if err = buffersrcCtx.BuffersrcAddFrame(decodeFrame, astiav.NewBuffersrcFlags(astiav.BuffersrcFlagKeepRef)); err != nil {
				err = fmt.Errorf("main: adding frame failed: %w", err)
				return
			}

			for{
				filterFrame.Unref()

				if err = buffersinkCtx.BuffersinkGetFrame(filterFrame, astiav.NewBuffersinkFlags()); err != nil {
					if errors.Is(err, astiav.ErrEof) || errors.Is(err, astiav.ErrEagain) {
						break
					}
					panic(err)
				}
	
				pts++
				filterFrame.SetPts(pts)
	
				// Encode the frame
				if err = encodeCodecContext.SendFrame(filterFrame); err != nil {
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

	// Find decoder
	decodeCodec := astiav.FindDecoder(videoStream.CodecParameters().CodecID())
	if decodeCodec == nil {
		panic("FindDecoder returned nil")
	}

	// Find decoder
	if decodeCodecContext = astiav.AllocCodecContext(decodeCodec); decodeCodecContext == nil {
		panic(err)
	}

	// Update codec context
	if err = videoStream.CodecParameters().ToCodecContext(decodeCodecContext); err != nil {
		panic(err)
	}

	// Set framerate
	decodeCodecContext.SetFramerate(inputFormatContext.GuessFrameRate(videoStream, nil))

	// Open codec context
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
	
	// Find encoder
	h264Encoder := astiav.FindEncoder(astiav.CodecIDH264)
	if h264Encoder == nil {
		panic("No H264 Encoder Found")
	}

	// Alloc codec context
	if encodeCodecContext = astiav.AllocCodecContext(h264Encoder); encodeCodecContext == nil {
		panic("Failed to AllocCodecContext Decoder")
	}

	// Update codec context
	encodeCodecContext.SetPixelFormat(astiav.PixelFormatYuv420P)
	encodeCodecContext.SetSampleAspectRatio(decodeCodecContext.SampleAspectRatio())
	encodeCodecContext.SetTimeBase(astiav.NewRational(1, 30))
	encodeCodecContext.SetWidth(decodeCodecContext.Width())
	encodeCodecContext.SetHeight(decodeCodecContext.Height())

	// Open codec context
	if err = encodeCodecContext.Open(h264Encoder, nil); err != nil {
		panic(err)
	}
}

func initFilters() {
	filterGraph = astiav.AllocFilterGraph()
	if filterGraph == nil {
		panic("filtergraph could not be created")
	}

	// Alloc outputs
	outputs := astiav.AllocFilterInOut()
	if outputs == nil {
		err = errors.New("main: outputs is nil")
		return
	}

	// Alloc inputs
	inputs := astiav.AllocFilterInOut()
	if inputs == nil {
		err = errors.New("main: inputs is nil")
		return
	}

	// Create buffersrc and buffersink filter contexts
	buffersrc := astiav.FindFilterByName("buffer")
	if buffersrc == nil {
		panic("buffersrc is nil")
	}

	buffersink := astiav.FindFilterByName("buffersink")
	if buffersink == nil {
		panic("buffersink is nil")
	}

	var err error
	if buffersrcCtx, err = filterGraph.NewFilterContext(buffersrc, "in", astiav.FilterArgs{
		"pix_fmt":      strconv.Itoa(int(decodeCodecContext.PixelFormat())),
		"video_size":   strconv.Itoa(decodeCodecContext.Width()) + "x" + strconv.Itoa(decodeCodecContext.Height()),
		"time_base":    videoStream.TimeBase().String(),
	}); err != nil {
		panic(err)
	}

	if buffersinkCtx, err = filterGraph.NewFilterContext(buffersink, "in", nil); err != nil {
		err = fmt.Errorf("main: creating buffersink context failed: %w", err)
		return
	}

	// Update outputs
	outputs.SetName("in")
	outputs.SetFilterContext(buffersrcCtx)
	outputs.SetPadIdx(0)
	outputs.SetNext(nil)

	// Update inputs
	inputs.SetName("out")
	inputs.SetFilterContext(buffersinkCtx)
	inputs.SetPadIdx(0)
	inputs.SetNext(nil)

	// Link buffersrc and buffersink through the eq filter for brightness
	if err = filterGraph.Parse("eq=brightness=0.5", inputs, outputs); err != nil {
		panic(err)
	}

	if err = filterGraph.Configure(); err != nil {
		err = fmt.Errorf("main: configuring filter failed: %w", err)
		return
	}

	filterFrame = astiav.AllocFrame()
}

func freeVideoCoding() {
	inputFormatContext.CloseInput()
	inputFormatContext.Free()

	decodeCodecContext.Free()
	decodePacket.Free()
	decodeFrame.Free()

	encodeCodecContext.Free()
	encodePacket.Free()
}