# Remainwith SFU

Remainwith is a robust, high-performance Selective Forwarding Unit (SFU) written in Go, designed to power scalable real-time video conferencing applications. Built on top of the [Pion WebRTC](https://github.com/pion/webrtc) library, it provides a stable backend for routing media streams between peers in a room-based architecture.

## Overview

The SFU acts as a media router. Instead of mixing streams (MCU), it receives media tracks from publishers and selectively forwards them to subscribers. This architecture significantly reduces server-side CPU usage compared to mixing, allowing for higher scalability.

Remainwith implements a Pub/Sub model for track management, ensuring that new participants automatically subscribe to existing tracks in a room, and existing participants receive tracks from new joiners.

## Key Features

### Core Capabilities
-   **Room-Based Routing**: Isolates peers into discrete rooms managed by a `TracksManager`.
-   **Dynamic Track Discovery**: Uses a Pub/Sub system to notify peers of new audio/video tracks.
-   **Codec Support**:
    -   **Video**: VP8 with RTX (Retransmission) support for packet loss resilience.
    -   **Audio**: Opus.
-   **Congestion Control**: Implements RTCP feedback loops including:
    -   **PLI (Picture Loss Indication)**: For requesting keyframes when video freezes or a new peer joins.
    -   **NACK**: For recovering lost packets.
    -   **REMB**: For bandwidth estimation.

### Network Stability & Connectivity
-   **ICE Restart**: Automatically detects connection failures and triggers ICE restarts to recover media flows without refreshing the page.
-   **Connection Monitoring**: Actively monitors peer connection states and sends keepalives to detect zombies or disconnected clients.
-   **Port Forwarding Support**: Optimized configuration for environments behind NATs, VS Code Tunnels, or ngrok, prioritizing TURN relays to ensure connectivity where direct P2P fails.
-   **STUN/TURN Integration**: Configured with multiple redundancy layers (Google STUN, Cloudflare STUN, and OpenRelay TURN).

## Architecture

### Components

1.  **SFU (`sfu.go`)**:
    -   The main entry point handling WebSocket connections.
    -   Manages the signaling state machine (Offer/Answer exchange).
    -   Handles ICE candidate trickling and buffering.
    -   Monitors connection health and manages the lifecycle of `Clients`.

2.  **TracksManager (`tracks_manager.go`)**:
    -   A global registry that maps Room IDs to `PeerManager` instances.
    -   Handles the creation and cleanup of rooms based on activity.
    -   Implements aggressive cleanup policies for empty or inactive rooms to prevent resource leaks.

3.  **PeerManager (`peer_manager.go`)**:
    -   Manages the list of peers within a specific room.
    -   Handles the subscription logic, ensuring peers don't subscribe to their own tracks.
    -   Forwards RTCP packets (like PLI) from subscribers back to publishers to ensure video quality.
    -   Prevents duplicate track readers using a registry system.

4.  **PubSub (`pubsub/`)**:
    -   An internal event bus that decouples track producers from consumers.
    -   Allows the system to scale track events asynchronously.

## Signaling Protocol

The SFU uses JSON messages over WebSockets for signaling.

### Message Types
-   **`ready`**: Sent by the client to indicate it is ready to receive offers.
-   **`offer`**: SDP offer sent by the SFU (for renegotiation) or Client (for initial connection).
-   **`answer`**: SDP answer sent in response to an offer.
-   **`candidate`**: ICE candidate for connectivity establishment.
-   **`sub_track` / `unsub_track`**: (Optional) Manual control for track subscription.

### Connection Flow
1.  **Handshake**: Client connects via WebSocket with `room_id` and `client_id`.
2.  **Transport Setup**: SFU initializes a WebRTC transport with configured ICE servers.
3.  **Negotiation**:
    -   Client sends `ready`.
    -   Client sends `offer` containing its local tracks.
    -   SFU matches tracks, sets up `recvonly` transceivers, and responds with `answer`.
4.  **Subscription**:
    -   When a new track is published, the SFU's PubSub system triggers an event.
    -   The SFU creates a `sendonly` transceiver on the subscriber's PeerConnection.
    -   The SFU sends a new `offer` to the subscriber to negotiate the new track.

## Technical Details

### Transceiver Direction
To ensure proper media flow and compatibility with browser behaviors:
-   **Ingress (Client -> SFU)**: The SFU configures transceivers as `recvonly`. The client sends media, the SFU receives.
-   **Egress (SFU -> Client)**: The SFU configures transceivers as `sendonly`. The SFU forwards media, the client receives.

### Error Handling & Recovery
-   **Pending Candidates**: ICE candidates arriving before the remote description is set are queued and processed immediately after the answer is applied.
-   **Track Timeout**: If a client connects but sends no media within 15 seconds, a warning is logged to assist in debugging frontend permission issues.
-   **Zombie Cleanup**: Rooms with no active peers are cleaned up after 30 seconds. Inactive rooms are cleaned up after 5 minutes.

### Media Processing
-   **RTCP Forwarding**: The SFU reads RTCP packets from subscribers. If a PLI (Picture Loss Indication) is received, it is forwarded to the publisher. This is critical for video streams; without it, a new subscriber might see a frozen frame until the next natural keyframe is generated.
-   **Track Registry**: The `PeerManager` maintains a `trackReaders` map to ensure that a specific track is only processed once, preventing duplicate processing routines that could degrade performance.

## Development

### Prerequisites
-   Go 1.20+
-   A modern web browser (Chrome/Firefox/Safari)

### Environment Variables
While the current implementation uses hardcoded ICE servers for ease of development, production deployments should configure TURN credentials via environment variables or a config file.

### Common Issues & Fixes
-   **"Codec not supported"**: Ensure the frontend and backend share the exact same codec payload types (VP8: 96, RTX: 97, Opus: 111).
-   **Black Video**: Often caused by missing PLI handling. The SFU explicitly forwards PLI packets to ensure keyframes are generated when a new subscriber joins.
-   **Port Forwarding Issues**: If using VS Code tunnels or ngrok, the SFU is configured to prioritize TURN relays to bypass strict NAT/Firewall rules that block direct P2P connections.