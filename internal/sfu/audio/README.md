# Pure Go Audio Processing Module

This module provides server-side audio processing for WebRTC calls, implemented entirely in Go without external C dependencies.

## Features

### 1. Echo Cancellation (AEC)
- **Algorithm**: Normalized Least Mean Squares (NLMS)
- **Purpose**: Removes speaker audio that leaks into the microphone
- **How it works**: 
  - Models the echo path using an adaptive filter
  - Subtracts estimated echo from microphone signal
  - Continuously adapts to changing acoustic conditions

### 2. Noise Suppression (NS)
- **Algorithm**: Envelope follower with noise gating
- **Purpose**: Reduces background noise while preserving speech clarity
- **How it works**:
  - Tracks signal envelope with separate attack/release times
  - Applies gain reduction when signal is below threshold
  - Maintains noise floor estimate for adaptive thresholding

### 3. Automatic Gain Control (AGC)
- **Algorithm**: RMS-based gain control
- **Purpose**: Normalizes audio levels automatically
- **How it works**:
  - Measures RMS level over a sliding window
  - Adjusts gain to reach target level
  - Respects min/max gain limits
  - Smooth gain changes with attack/release

### 4. High-Pass Filter (HPF)
- **Algorithm**: First-order IIR filter
- **Purpose**: Removes DC offset and low-frequency noise
- **How it works**:
  - Simple recursive filter with configurable cutoff
  - Default cutoff: 80Hz

## Usage

### Basic Processing
```go
import "Remainwith/internal/sfu/audio"

// Create processor with default config
config := audio.DefaultConfig()
processor, err := audio.NewProcessor(config)
if err != nil {
    log.Fatal(err)
}
defer processor.Close()

// Process audio frame (float32 samples in range [-1.0, 1.0])
samples := []float32{0.5, -0.3, 0.8, ...}
processed, err := processor.ProcessFrame(samples)
```

### For Recording (No Echo Cancellation)
```go
pipeline, err := audio.NewRecordingPipeline(true)
if err != nil {
    log.Fatal(err)
}
defer pipeline.Close()

// Process PCM data (int16 format)
pcmData := []byte{...} // Raw PCM bytes
processed, err := pipeline.ProcessAudio(pcmData)
```

### For Real-time Calls (With AEC)
```go
cp, err := audio.NewCallProcessor(true)
if err != nil {
    log.Fatal(err)
}
defer cp.Close()

// Process far-end audio (speaker output)
farEndSamples := []float32{...}
cp.ProcessFarEnd(farEndSamples)

// Process near-end audio (microphone input)
nearEndSamples := []float32{...}
processed, err := cp.ProcessNearEnd(nearEndSamples)
```

## Configuration

```go
config := audio.ProcessorConfig{
    Enabled:              true,
    EchoCancellation:     true,
    NoiseSuppression:     true,
    AutomaticGainControl: true,
    HighPassFilter:       true,
    
    // AEC settings
    AECTailLengthMs: 100,  // Echo tail length
    AECStepSize:     0.5,  // Adaptation speed (0.0-1.0)
    
    // NS settings
    NSThresholdDb: -40,    // Noise gate threshold
    NSAttackMs:    10,      // Attack time
    NSReleaseMs:   100,     // Release time
    
    // AGC settings
    AGCTargetDb:  -20,      // Target level in dB
    AGCMaxGainDb: 30,       // Maximum gain
    AGCMinGainDb: -10,      // Minimum gain
    AGCAttackMs:  50,       // Attack time
    AGCReleaseMs: 500,      // Release time
    
    // HPF settings
    HPCutoffHz: 80,         // Cutoff frequency
}
```

## Performance

Benchmarks on AMD Ryzen 5 5600H:
- Full pipeline (AEC+NS+AGC+HPF): ~10ms per 10ms frame
- AEC only: ~9.5ms per 10ms frame

**Note**: This is borderline for real-time processing. For production use with many concurrent calls, consider:
1. Using a more optimized AEC implementation
2. Processing audio in larger chunks (20-40ms)
3. Running on dedicated CPU cores

## Architecture

```
┌─────────────────────────────────────────┐
│           Audio Processor               │
├─────────────────────────────────────────┤
│  Input: []float32 (48kHz, mono)        │
├─────────────────────────────────────────┤
│  1. High-Pass Filter (HPF)              │
│     - Remove DC and low freq noise      │
├─────────────────────────────────────────┤
│  2. Echo Cancellation (AEC)             │
│     - NLMS adaptive filter              │
│     - Requires far-end reference        │
├─────────────────────────────────────────┤
│  3. Noise Suppression (NS)              │
│     - Envelope follower gate            │
├─────────────────────────────────────────┤
│  4. Automatic Gain Control (AGC)        │
│     - RMS-based level normalization     │
├─────────────────────────────────────────┤
│  Output: []float32 (processed)           │
└─────────────────────────────────────────┘
```

## Testing

Run tests:
```bash
go test ./internal/sfu/audio/... -v
```

Run benchmarks:
```bash
go test ./internal/sfu/audio/... -bench=. -benchtime=1s
```

## Implementation Notes

1. **Pure Go**: No CGO or external dependencies
2. **Thread-safe**: Uses mutex for concurrent access
3. **Real-time optimized**: Minimal allocations in hot path
4. **Configurable**: All parameters tunable via config
5. **Graceful degradation**: Continues working if features disabled

## Future Improvements

1. **Optimized AEC**: Use frequency-domain (FFT) NLMS for better performance
2. **Neural NS**: Consider lightweight ML-based noise suppression
3. **Multi-channel**: Support stereo processing
4. **SIMD**: Use Go's SIMD instructions for vector operations
5. **Profiling**: Add metrics and performance monitoring
