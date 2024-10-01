package client

import (
	"fmt"
	"log"

	"github.com/gorilla/websocket"
	"github.com/pion/webrtc/v3"
)

type Message struct {
    Type    string `json:"type"`
    Content string `json:"content"`
}

var (
    answerChan = make(chan string) // Global variable for the channel
    userPeerConnection *webrtc.PeerConnection
    connectionEstablishedChan = make(chan bool)
)

func createPeerConnection() (*webrtc.PeerConnection, error) {
    config := webrtc.Configuration{
        ICEServers: []webrtc.ICEServer{
            {URLs: []string{"stun:stun.l.google.com:19302", "stun:stun1.l.google.com:19302", "stun:stun2.l.google.com:19302"}}, // Add your STUN server here
        },
    }

	// Create a new RTCPeerConnection
	peerConnection, err := webrtc.NewPeerConnection(config)
	if err != nil {
		return nil, err
	}

	// Set the handler for ICE connection state
	// This will notify you when the peer has connected/disconnected
	peerConnection.OnICEConnectionStateChange(func(connectionState webrtc.ICEConnectionState) {
        fmt.Printf("Connection State has changed: %s\n", connectionState.String())
        switch connectionState {
        case webrtc.ICEConnectionStateConnected:
            fmt.Println("Successfully connected!")
        case webrtc.ICEConnectionStateDisconnected:
            fmt.Println("Disconnected!")
        case webrtc.ICEConnectionStateFailed:
            fmt.Println("Connection failed!")
        }    
	})
    
    return peerConnection, nil 
}

func openCameraFeed(peerConnection *webrtc.PeerConnection) error {
    // Implement camera capture and streaming logic here
    // This is a placeholder and should be replaced with actual video capture logic
    videoTrack, err := webrtc.NewTrackLocalStaticSample(webrtc.RTPCodecCapability{MimeType: "video/h264"}, "video", "pion")
    if err != nil {
        return err
    }

    // Add the track to the peer connection
    _, err = peerConnection.AddTrack(videoTrack)
    if err != nil {
        return err
    }

    // Handle incoming tracks
    peerConnection.OnTrack(func(track *webrtc.TrackRemote, receiver *webrtc.RTPReceiver) {
        fmt.Println("Track received:", track.Kind())
        go func() {
            for {
                // Read frames from the track
                _, _, err := track.ReadRTP()
                if err != nil {
                    log.Println("Error reading RTP:", err)
                    return
                }
                // Handle the frames as needed
                // Here we would typically render them to a video element
            }
        }()
    })

    fmt.Println("Tracks are setup. Starting to write to them...")
    go writeH264ToTrack(videoTrack)

    return nil
}


func establishConnectionWithPeer(conn *websocket.Conn){
    peerConnection, err := createPeerConnection()
    if err != nil {
        panic(err)
    }

	// Create offer 
    offer, err := peerConnection.CreateOffer(nil)
    if err != nil {
        log.Fatal("Failed to create offer:", err)
    }

    // Set the local description
    if err = peerConnection.SetLocalDescription(offer); err != nil {
        log.Fatal("Failed to set local description:", err)
    }

    offerMsg := Message{
        Type:    "offer",
        Content: offer.SDP,
    }
    // fmt.Println(offerMsg)
    conn.WriteJSON(offerMsg)

    answer := <-answerChan
    fmt.Println("Setting remote description with answer.")

    // Set the remote description
    answerSDP := webrtc.SessionDescription{
        Type: webrtc.SDPTypeAnswer,
        SDP:  answer,
    }
    
    if err := peerConnection.SetRemoteDescription(answerSDP); err != nil {
        log.Fatal("Failed to set remote description:", err)
    }

    userPeerConnection = peerConnection
    connectionEstablishedChan <- true
}

func handleOffer(conn *websocket.Conn, msg Message){
    fmt.Println("Received offer")
    peerConnection, err := createPeerConnection()
    if err != nil {
        panic(err)
    }
    offerSDP := webrtc.SessionDescription{
        Type: webrtc.SDPTypeOffer,
        SDP: msg.Content,
    }
    
    if err := peerConnection.SetRemoteDescription(offerSDP); err != nil {
        log.Fatal("Failed to set remote description:", err)
    }

	// Create answer 
    answer, err := peerConnection.CreateAnswer(nil)
    if err != nil {
        log.Fatal("Failed to create answer:", err)
    }

    // Set the local description
    if err = peerConnection.SetLocalDescription(answer); err != nil {
        log.Fatal("Failed to set local description:", err)
    }

    answerMsg := Message{
        Type:    "answer",
        Content: answer.SDP,
    }
    // fmt.Println(answerMsg)
    conn.WriteJSON(answerMsg)

    userPeerConnection = peerConnection
    connectionEstablishedChan <- true
}

func handleAnswer(conn *websocket.Conn, msg Message){
    answerChan <- msg.Content
}

func Run() {
    // Connect to the WebSocket server
    url := "ws://localhost:8080/ws"
    conn, _, err := websocket.DefaultDialer.Dial(url, nil)
    if err != nil {
        log.Fatal("Dial error:", err)
    }
    defer conn.Close()

    fmt.Println("Connected to the server")

    // Start a goroutine to listen for messages from the server
    go func(conn *websocket.Conn) {
        var inputMsg Message
        for {
            err := conn.ReadJSON(&inputMsg)
            if err != nil {
                log.Println("Read error:", err)
                return
            }
            fmt.Printf("Message from server: %s\n", inputMsg.Type)
            if (inputMsg.Type == "join"){
                go establishConnectionWithPeer(conn)
            } else if (inputMsg.Type == "offer"){
                go handleOffer(conn, inputMsg)
            } else if (inputMsg.Type == "answer"){
                go handleAnswer(conn, inputMsg)
            }
        }
    }(conn)

    msg := Message{
        Type:    "join",
        Content: "true",
    }
    // fmt.Println(msg)
    conn.WriteJSON(msg)


    <-connectionEstablishedChan
    fmt.Println("Successfully established a WebRTC connection between clients")

    openCameraFeed(userPeerConnection)

	select {}
}