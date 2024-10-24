package client

import (
	"errors"
	"fmt"
	"log"
	"strconv"
	"time"

	"github.com/asticode/go-astiav"
)

type VideoProcessor struct {
	inputFormatContext *astiav.FormatContext

	videoStream *astiav.Stream

	decodeCodecContext *astiav.CodecContext
	decodePacket       *astiav.Packet
	decodeFrame        *astiav.Frame

	encodeCodecContext *astiav.CodecContext
	encodePacket *astiav.Packet

	convertToRGBAContext *astiav.SoftwareScaleContext
	convertToYUV420PContext *astiav.SoftwareScaleContext
	rgbaFrame *astiav.Frame
	yuv420PFrame *astiav.Frame

	filterGraph *astiav.FilterGraph
	filterFrame *astiav.Frame
	buffersinkContext *astiav.BuffersinkFilterContext
	buffersrcContext  *astiav.BuffersrcFilterContext

	arFilterFrame *astiav.Frame

	pts int64
}

const h264FrameDuration = time.Millisecond * 20

func NewVideoProcessor() *VideoProcessor {
	vp := &VideoProcessor{}

	astiav.RegisterAllDevices()

	if err := vp.initTestSrc(); err != nil{
		log.Fatal("Failed to initialize source: ", err)
	}
	if err := vp.initVideoEncoding(); err != nil{
		log.Fatal("Failed to initialize video encoding: ", err)
	}

	return vp
}

func (vp *VideoProcessor) initTestSrc() error {
	if vp.inputFormatContext = astiav.AllocFormatContext(); vp.inputFormatContext == nil {
		return errors.New("Failed to AllocCodecContext")
	}

	// Open input
	if err := vp.inputFormatContext.OpenInput("udp://224.0.0.251:5353", nil, nil); err != nil {
		return err
	}

	// Find stream info
	if err := vp.inputFormatContext.FindStreamInfo(nil); err != nil {
		return err
	}

	// Set stream
	vp.videoStream = vp.inputFormatContext.Streams()[0]

	// Find decoder
	decoder := astiav.FindDecoder(vp.videoStream.CodecParameters().CodecID())
	if decoder == nil {
		return errors.New("FindDecoder returned nil")
	}

	// Allocate decoding context
	if vp.decodeCodecContext = astiav.AllocCodecContext(decoder); vp.decodeCodecContext == nil {
		return errors.New("Failed to allocate context for decoder")
	}

	// Update codec context of video stream
	if err := vp.videoStream.CodecParameters().ToCodecContext(vp.decodeCodecContext); err != nil {
		return err
	}

	// Set framerate
	vp.decodeCodecContext.SetFramerate(vp.inputFormatContext.GuessFrameRate(vp.videoStream, nil))

	// Open decoding codec context
	if err := vp.decodeCodecContext.Open(decoder, nil); err != nil {
		panic(err)
	}

	vp.decodePacket = astiav.AllocPacket()
	vp.decodeFrame = astiav.AllocFrame()

	return nil
}

func (vp *VideoProcessor) initVideoEncoding() error {
	if vp.encodeCodecContext != nil {
		return nil
	}

	// Find H264 encoder
	h264Encoder := astiav.FindEncoder(astiav.CodecIDH264)
	if h264Encoder == nil {
		return errors.New("No H264 Encoder Found")
	}

	// Allocate encoding codec context
	if vp.encodeCodecContext = astiav.AllocCodecContext(h264Encoder); vp.encodeCodecContext == nil {
		return errors.New("Failed to AllocCodecContext Decoder")
	}

	// Update encoding codec context
	vp.encodeCodecContext.SetPixelFormat(astiav.PixelFormatYuv420P)
	vp.encodeCodecContext.SetSampleAspectRatio(vp.decodeCodecContext.SampleAspectRatio())
	vp.encodeCodecContext.SetTimeBase(astiav.NewRational(1, 30))
	vp.encodeCodecContext.SetWidth(vp.decodeCodecContext.Width())
	vp.encodeCodecContext.SetHeight(vp.decodeCodecContext.Height())

	// Open encoding codec context
	err := vp.encodeCodecContext.Open(h264Encoder, nil); 
	if err != nil {
		return err
	}
	
	// create a scale context to convert frames to RGBA format
	vp.convertToRGBAContext, err = astiav.CreateSoftwareScaleContext(
		vp.decodeCodecContext.Width(),
		vp.decodeCodecContext.Height(),
		vp.decodeCodecContext.PixelFormat(),
		vp.decodeCodecContext.Width(),
		vp.decodeCodecContext.Height(),
		astiav.PixelFormatRgba,
		astiav.NewSoftwareScaleContextFlags(astiav.SoftwareScaleContextFlagBilinear),
	)

	if err != nil {
		return err
	}

	// create a scale context to convert frames to YUV420P format
	vp.convertToYUV420PContext, err = astiav.CreateSoftwareScaleContext(
		vp.convertToRGBAContext.DestinationWidth(),
		vp.convertToRGBAContext.DestinationHeight(),
		vp.convertToRGBAContext.DestinationPixelFormat(),
		vp.convertToRGBAContext.SourceWidth(),
		vp.convertToRGBAContext.SourceHeight(),
		astiav.PixelFormatYuv420P,
		vp.convertToRGBAContext.Flags(),
	)

	if err != nil {
		return err
	}

	vp.rgbaFrame = astiav.AllocFrame()
	vp.yuv420PFrame = astiav.AllocFrame()

	return nil
}

func (vp *VideoProcessor) initFilters() error {
	if vp.filterGraph = astiav.AllocFilterGraph(); vp.filterGraph == nil {
		return errors.New("filtergraph could not be created")
	}

	// Alloc outputs
	outputs := astiav.AllocFilterInOut()
	if outputs == nil {
		return errors.New("main: outputs is nil")
	}

	// Alloc inputs
	inputs := astiav.AllocFilterInOut()
	if inputs == nil {
		return errors.New("main: inputs is nil")
	}

	// Create source buffer filters
	buffersrc := astiav.FindFilterByName("buffer")
	if buffersrc == nil {
		return errors.New("buffersrc is nil")
	}

	// Create sink buffer filters
	buffersink := astiav.FindFilterByName("buffersink")
	if buffersink == nil {
		return errors.New("buffersink is nil")
	}

	// Create filter contexts 
	var err error
	if vp.buffersrcContext, err = vp.filterGraph.NewBuffersrcFilterContext(
		buffersrc, 
		"in", 
		astiav.FilterArgs{
			"pix_fmt":      strconv.Itoa(int(vp.decodeCodecContext.PixelFormat())),
			"video_size":   strconv.Itoa(vp.decodeCodecContext.Width()) + "x" + strconv.Itoa(vp.decodeCodecContext.Height()),
			"time_base":    vp.videoStream.TimeBase().String(),
		}); err != nil {
			return err
		}

	if vp.buffersinkContext, err = vp.filterGraph.NewBuffersinkFilterContext(
		buffersink, 
		"in", 
		nil); err != nil {
		return fmt.Errorf("main: creating buffersink context failed: %w", err)
	}

	// Update outputs
	outputs.SetName("in")
	outputs.SetFilterContext(vp.buffersrcContext.FilterContext())
	outputs.SetPadIdx(0)
	outputs.SetNext(nil)

	// Update inputs
	inputs.SetName("out")
	inputs.SetFilterContext(vp.buffersinkContext.FilterContext())
	inputs.SetPadIdx(0)
	inputs.SetNext(nil)

	// Link buffersrc and buffersink through the eq filter for brightness
	if err := vp.filterGraph.Parse("eq=brightness=0.5", inputs, outputs); err != nil {
		return err
	}

	if err := vp.filterGraph.Configure(); err != nil {
		return fmt.Errorf("main: configuring filter failed: %w", err)
	}

	// Allocate frame to store the filtered contents
	vp.filterFrame = astiav.AllocFrame()

	return nil
}

func (vp *VideoProcessor) freeVideoCoding() {
	vp.inputFormatContext.CloseInput()
	vp.inputFormatContext.Free()

	vp.decodeCodecContext.Free()
	vp.decodePacket.Free()
	vp.decodeFrame.Free()

	vp.encodeCodecContext.Free()
	vp.encodePacket.Free()

	vp.convertToRGBAContext.Free()
	vp.convertToYUV420PContext.Free()
	vp.rgbaFrame.Free()
	vp.yuv420PFrame.Free()

	vp.buffersrcContext.FilterContext().Free()
	vp.buffersinkContext.FilterContext().Free()
	vp.filterFrame.Free()
	vp.filterGraph.Free()
}