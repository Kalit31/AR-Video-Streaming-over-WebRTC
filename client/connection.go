package client

import (
	"fmt"
	"log"

	"github.com/gorilla/websocket"
	"github.com/pion/webrtc/v3"
)

var (
	userPeerConnection      *webrtc.PeerConnection
	userVideoTrack          *webrtc.TrackLocalStaticSample
	connectionEstablishedChan = make(chan bool)
)

func createPeerConnection(conn *websocket.Conn) (*webrtc.PeerConnection, *webrtc.TrackLocalStaticSample, error) {
    /*
	Initializes a new WebRTC peer connection
	*/
	
	config := webrtc.Configuration{
        ICEServers: []webrtc.ICEServer{
            {
                URLs: []string{
                    "stun:stun.l.google.com:19302", 
                    "stun:stun1.l.google.com:19302", 
                    "stun:stun2.l.google.com:19302",
                },
            }, 
        },
    }

	// Create a new RTCPeerConnection
	peerConnection, err := webrtc.NewPeerConnection(config)
	if err != nil {
		return nil, nil, err
	}

    peerConnection.OnICECandidate(func(candidate *webrtc.ICECandidate) {
        if candidate == nil {
            return
        }
        // Send this candidate to the remote peer
        fmt.Println("New ICE candidate:", candidate.ToJSON())
        iceCandidateMsg := Message{
            Type:    "iceCandidate",
            Content: candidate.ToJSON().Candidate,
        }
        conn.WriteJSON(iceCandidateMsg)
    })

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


    videoTrack, err := webrtc.NewTrackLocalStaticSample(webrtc.RTPCodecCapability{MimeType: "video/h264"}, "video", "pion")
    if err != nil {
        return nil, nil, err
    }

    // Add the track to the peer connection
    _, err = peerConnection.AddTrack(videoTrack)
    if err != nil {
        return nil, nil, err
    }
    
    return peerConnection, videoTrack, nil 
}

func establishConnectionWithPeer(conn *websocket.Conn){
    peerConnection, videoTrack, err := createPeerConnection(conn)
    if err != nil {
        panic(err)
    }

	// Create offer 
    offer, err := peerConnection.CreateOffer(nil)
    if err != nil {
        log.Fatal("Failed to create offer: ", err)
    }

    // Set the local description
    if err = peerConnection.SetLocalDescription(offer); err != nil {
        log.Fatal("Failed to set local description: ", err)
    }

    offerMsg := Message{
        Type:    "offer",
        Content: offer.SDP,
    }
	conn.WriteJSON(offerMsg)

    answer := <-answerChan
    fmt.Println("Setting remote description with answer.")

    // Set the remote description
    answerSDP := webrtc.SessionDescription{
        Type: webrtc.SDPTypeAnswer,
        SDP:  answer,
    }
    
    if err := peerConnection.SetRemoteDescription(answerSDP); err != nil {
        log.Fatal("Failed to set remote description: ", err)
    }

    userPeerConnection = peerConnection
    userVideoTrack = videoTrack
    connectionEstablishedChan <- true
}

