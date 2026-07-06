// Package audio provides server-side audio processing in pure Go
// Implements: Echo Cancellation, Noise Suppression, AGC, and High-Pass Filter
package audio

import (
	"log"
	"math"
	"sync"
	"time"
)

// ProcessorConfig configures the audio processing pipeline
type ProcessorConfig struct {
	// Enable processing
	Enabled bool

	// Processing features
	EchoCancellation     bool // AEC - removes speaker audio from mic
	NoiseSuppression     bool // NS - reduces background noise
	AutomaticGainControl bool // AGC - normalizes audio levels
	HighPassFilter       bool // HPF - removes low-frequency noise
	FeedbackSuppression  bool // FBS - suppresses acoustic feedback/howling
	FeedbackDetection    bool // FBD - detects feedback frequencies

	// AEC settings
	AECTailLengthMs int     // Echo tail length in milliseconds
	AECStepSize     float64 // NLMS step size (0.0 to 1.0)

	// NS settings
	NSThresholdDb float64 // Noise gate threshold in dB
	NSAttackMs    int     // Attack time in ms
	NSReleaseMs   int     // Release time in ms

	// AGC settings
	AGCTargetDb  float64 // Target level in dB
	AGCMaxGainDb float64 // Maximum gain in dB
	AGCMinGainDb float64 // Minimum gain in dB
	AGCAttackMs  int     // Attack time in ms
	AGCReleaseMs int     // Release time in ms

	// HPF settings
	HPCutoffHz float64 // High-pass filter cutoff frequency

	// Feedback suppression settings
	FBMaxNotches         int     // Maximum number of notch filters
	FBDetectionThreshold float64 // Feedback detection threshold (0.0 to 1.0)
	FBNotchQ             float64 // Notch filter Q factor (higher = narrower)
	FBMinFreqHz          float64 // Minimum feedback frequency to suppress
	FBMaxFreqHz          float64 // Maximum feedback frequency to suppress
}

// DefaultConfig returns a balanced configuration
func DefaultConfig() ProcessorConfig {
	return ProcessorConfig{
		Enabled:              true,
		EchoCancellation:     true,
		NoiseSuppression:     true,
		AutomaticGainControl: true,
		HighPassFilter:       true,
		FeedbackSuppression:  true,
		FeedbackDetection:    true,

		// AEC defaults
		AECTailLengthMs: 100,
		AECStepSize:     0.5,

		// NS defaults
		NSThresholdDb: -40,
		NSAttackMs:    10,
		NSReleaseMs:   100,

		// AGC defaults
		AGCTargetDb:  -20,
		AGCMaxGainDb: 30,
		AGCMinGainDb: -10,
		AGCAttackMs:  50,
		AGCReleaseMs: 500,

		// HPF defaults
		HPCutoffHz: 80,

		// Feedback suppression defaults
		FBMaxNotches:         8,
		FBDetectionThreshold: 0.7,
		FBNotchQ:             10.0,
		FBMinFreqHz:          200,
		FBMaxFreqHz:          8000,
	}
}

// Processor handles audio processing
type Processor struct {
	config ProcessorConfig
	mu     sync.RWMutex

	// AEC state
	aec *AECProcessor

	// NS state
	ns *NSProcessor

	// AGC state
	agc *AGCProcessor

	// HPF state
	hpf *HPFProcessor

	// Feedback suppression state
	fbs *FeedbackSuppressionProcessor

	// Sample rate (fixed at 48kHz for Opus)
	sampleRate int
}

// NewProcessor creates a new audio processor
func NewProcessor(config ProcessorConfig) (*Processor, error) {
	if !config.Enabled {
		return &Processor{config: config}, nil
	}

	sampleRate := 48000

	p := &Processor{
		config:     config,
		sampleRate: sampleRate,
	}

	// Initialize AEC
	if config.EchoCancellation {
		p.aec = NewAECProcessor(sampleRate, config.AECTailLengthMs, config.AECStepSize)
	}

	// Initialize NS
	if config.NoiseSuppression {
		p.ns = NewNSProcessor(sampleRate, config.NSThresholdDb, config.NSAttackMs, config.NSReleaseMs)
	}

	// Initialize AGC
	if config.AutomaticGainControl {
		p.agc = NewAGCProcessor(sampleRate, config.AGCTargetDb, config.AGCMaxGainDb, config.AGCMinGainDb, config.AGCAttackMs, config.AGCReleaseMs)
	}

	// Initialize HPF
	if config.HighPassFilter {
		p.hpf = NewHPFProcessor(sampleRate, config.HPCutoffHz)
	}

	// Initialize Feedback Suppression
	if config.FeedbackSuppression {
		p.fbs = NewFeedbackSuppressionProcessor(sampleRate, config.FBMaxNotches, config.FBNotchQ, config.FBMinFreqHz, config.FBMaxFreqHz)
	}

	log.Printf("AudioProcessor: Created with AEC=%v NS=%v AGC=%v HPF=%v FBS=%v",
		config.EchoCancellation,
		config.NoiseSuppression,
		config.AutomaticGainControl,
		config.HighPassFilter,
		config.FeedbackSuppression)

	return p, nil
}

// ProcessFrame processes a frame of float32 audio samples
// samples should be in range [-1.0, 1.0]
func (p *Processor) ProcessFrame(samples []float32) ([]float32, error) {
	if !p.config.Enabled {
		return samples, nil
	}

	p.mu.Lock()
	defer p.mu.Unlock()

	result := make([]float32, len(samples))
	copy(result, samples)

	// Apply HPF first (remove DC and low freq noise)
	if p.hpf != nil {
		p.hpf.Process(result)
	}

	// Apply Feedback Suppression (detect and remove howling frequencies)
	if p.fbs != nil {
		p.fbs.Process(result)
	}

	// Apply AEC (if we have reference signal)
	// Note: For proper AEC, ProcessRender must be called first with far-end audio
	if p.aec != nil {
		p.aec.ProcessCapture(result)
	}

	// Apply Noise Suppression
	if p.ns != nil {
		p.ns.Process(result)
	}

	// Apply AGC last (normalize levels)
	if p.agc != nil {
		p.agc.Process(result)
	}

	return result, nil
}

// ProcessRender processes far-end audio for AEC reference
// Must be called BEFORE ProcessCapture with the same timestamp
func (p *Processor) ProcessRender(samples []float32) {
	if !p.config.Enabled || p.aec == nil {
		return
	}

	p.mu.Lock()
	defer p.mu.Unlock()

	p.aec.ProcessRender(samples)
}

// ProcessCapture processes near-end audio with AEC
func (p *Processor) ProcessCapture(samples []float32) ([]float32, error) {
	return p.ProcessFrame(samples)
}

// ProcessBufferInt16 processes a buffer of int16 audio data
func (p *Processor) ProcessBufferInt16(buffer []byte) ([]byte, error) {
	if !p.config.Enabled {
		return buffer, nil
	}

	// Convert bytes to int16 samples
	samples := bytesToInt16(buffer)

	// Convert to float32
	floatSamples := int16ToFloat32(samples)

	// Process
	processed, err := p.ProcessFrame(floatSamples)
	if err != nil {
		return nil, err
	}

	// Convert back to int16
	intSamples := float32ToInt16(processed)

	// Convert back to bytes
	return int16ToBytes(intSamples), nil
}

// Close releases resources
func (p *Processor) Close() error {
	return nil
}

// ==================== AEC (Adaptive Echo Cancellation) ====================

// AECProcessor implements NLMS-based echo cancellation
type AECProcessor struct {
	// Filter coefficients (echo path model)
	filterCoeffs []float64
	filterLen    int

	// Reference buffer (far-end audio)
	refBuffer []float64
	refIndex  int

	// Step size for NLMS adaptation
	stepSize float64

	// Power estimate for normalization
	powerEstimate float64
	powerAlpha    float64
}

// NewAECProcessor creates a new AEC processor
func NewAECProcessor(sampleRate, tailLengthMs int, stepSize float64) *AECProcessor {
	// Calculate filter length from tail length
	filterLen := (sampleRate * tailLengthMs) / 1000

	return &AECProcessor{
		filterCoeffs: make([]float64, filterLen),
		filterLen:    filterLen,
		refBuffer:    make([]float64, filterLen),
		stepSize:     stepSize,
		powerAlpha:   0.99, // Power smoothing factor
	}
}

// ProcessRender processes far-end audio (reference signal)
func (a *AECProcessor) ProcessRender(samples []float32) {
	for _, s := range samples {
		// Store in reference buffer
		a.refBuffer[a.refIndex] = float64(s)
		a.refIndex = (a.refIndex + 1) % a.filterLen

		// Update power estimate
		a.powerEstimate = a.powerAlpha*a.powerEstimate + (1-a.powerAlpha)*float64(s)*float64(s)
	}
}

// ProcessCapture processes near-end audio and removes echo
func (a *AECProcessor) ProcessCapture(samples []float32) {
	for i := range samples {
		// Calculate filter output (estimated echo)
		echoEstimate := 0.0
		for j := 0; j < a.filterLen; j++ {
			idx := (a.refIndex - j - 1 + a.filterLen) % a.filterLen
			echoEstimate += a.filterCoeffs[j] * a.refBuffer[idx]
		}

		// Subtract echo estimate
		sample := float64(samples[i])
		errorSignal := sample - echoEstimate
		samples[i] = float32(errorSignal)

		// NLMS adaptation (only when there's reference power)
		if a.powerEstimate > 1e-10 {
			// Update coefficients
			normalization := a.powerEstimate + 1e-10
			for j := 0; j < a.filterLen; j++ {
				idx := (a.refIndex - j - 1 + a.filterLen) % a.filterLen
				a.filterCoeffs[j] += a.stepSize * errorSignal * a.refBuffer[idx] / normalization
			}
		}
	}
}

// ==================== NS (Noise Suppression) ====================

// NSProcessor implements spectral noise gating
type NSProcessor struct {
	// Noise gate threshold (linear)
	threshold float64

	// Envelope follower for smooth gating
	envelope     float64
	attackCoeff  float64
	releaseCoeff float64

	// Noise floor estimate
	noiseFloor float64
	noiseAlpha float64
}

// NewNSProcessor creates a new noise suppression processor
func NewNSProcessor(sampleRate int, thresholdDb float64, attackMs, releaseMs int) *NSProcessor {
	threshold := math.Pow(10, thresholdDb/20)

	// Calculate attack/release coefficients
	attackCoeff := 1.0 - math.Exp(-1.0/(float64(sampleRate)*float64(attackMs)/1000.0))
	releaseCoeff := 1.0 - math.Exp(-1.0/(float64(sampleRate)*float64(releaseMs)/1000.0))

	return &NSProcessor{
		threshold:    threshold,
		attackCoeff:  attackCoeff,
		releaseCoeff: releaseCoeff,
		noiseFloor:   threshold * 0.1,
		noiseAlpha:   0.95,
	}
}

// Process applies noise suppression
func (n *NSProcessor) Process(samples []float32) {
	for i := range samples {
		absSample := math.Abs(float64(samples[i]))

		// Update noise floor estimate
		if absSample < n.noiseFloor*2 {
			n.noiseFloor = n.noiseAlpha*n.noiseFloor + (1-n.noiseAlpha)*absSample
		}

		// Envelope follower
		if absSample > n.envelope {
			n.envelope += n.attackCoeff * (absSample - n.envelope)
		} else {
			n.envelope += n.releaseCoeff * (absSample - n.envelope)
		}

		// Noise gating
		if n.envelope < n.threshold+n.noiseFloor {
			// Below threshold - apply gain reduction
			gain := n.envelope / (n.threshold + n.noiseFloor)
			if gain < 0.01 {
				gain = 0.01
			}
			samples[i] = float32(float64(samples[i]) * gain * gain) // Square law for smoother gate
		}
	}
}

// ==================== AGC (Automatic Gain Control) ====================

// AGCProcessor implements RMS-based automatic gain control
type AGCProcessor struct {
	// Target level (linear)
	targetLevel float64

	// Gain limits
	maxGain float64
	minGain float64

	// Current gain
	currentGain float64

	// Attack/release coefficients
	attackCoeff  float64
	releaseCoeff float64

	// RMS measurement
	rmsWindow    []float64
	rmsIndex     int
	rmsSum       float64
	rmsWindowLen int
}

// NewAGCProcessor creates a new AGC processor
func NewAGCProcessor(sampleRate int, targetDb, maxGainDb, minGainDb float64, attackMs, releaseMs int) *AGCProcessor {
	targetLevel := math.Pow(10, targetDb/20)
	maxGain := math.Pow(10, maxGainDb/20)
	minGain := math.Pow(10, minGainDb/20)

	// Calculate attack/release coefficients
	attackCoeff := 1.0 - math.Exp(-1.0/(float64(sampleRate)*float64(attackMs)/1000.0))
	releaseCoeff := 1.0 - math.Exp(-1.0/(float64(sampleRate)*float64(releaseMs)/1000.0))

	// RMS window length (50ms)
	rmsWindowLen := sampleRate / 20

	return &AGCProcessor{
		targetLevel:  targetLevel,
		maxGain:      maxGain,
		minGain:      minGain,
		currentGain:  1.0,
		attackCoeff:  attackCoeff,
		releaseCoeff: releaseCoeff,
		rmsWindow:    make([]float64, rmsWindowLen),
		rmsWindowLen: rmsWindowLen,
	}
}

// Process applies automatic gain control
func (a *AGCProcessor) Process(samples []float32) {
	for i := range samples {
		sample := float64(samples[i])

		// Update RMS measurement
		a.rmsSum -= a.rmsWindow[a.rmsIndex]
		squared := sample * sample
		a.rmsWindow[a.rmsIndex] = squared
		a.rmsSum += squared
		a.rmsIndex = (a.rmsIndex + 1) % a.rmsWindowLen

		// Calculate RMS
		rms := math.Sqrt(a.rmsSum / float64(a.rmsWindowLen))

		// Calculate desired gain
		var desiredGain float64
		if rms > 1e-10 {
			desiredGain = a.targetLevel / rms
		} else {
			desiredGain = a.currentGain
		}

		// Clamp gain
		if desiredGain > a.maxGain {
			desiredGain = a.maxGain
		}
		if desiredGain < a.minGain {
			desiredGain = a.minGain
		}

		// Smooth gain changes
		if desiredGain > a.currentGain {
			a.currentGain += a.attackCoeff * (desiredGain - a.currentGain)
		} else {
			a.currentGain += a.releaseCoeff * (desiredGain - a.currentGain)
		}

		// Apply gain
		samples[i] = float32(sample * a.currentGain)
	}
}

// ==================== HPF (High-Pass Filter) ====================

// HPFProcessor implements a simple IIR high-pass filter
type HPFProcessor struct {
	// Filter coefficients
	a1 float64
	b0 float64
	b1 float64

	// State
	x1 float64 // Previous input
	y1 float64 // Previous output
}

// NewHPFProcessor creates a new high-pass filter
func NewHPFProcessor(sampleRate int, cutoffHz float64) *HPFProcessor {
	// Calculate filter coefficients (simple first-order HPF)
	// y[n] = b0*x[n] + b1*x[n-1] - a1*y[n-1]
	rc := 1.0 / (2.0 * math.Pi * cutoffHz)
	dt := 1.0 / float64(sampleRate)
	alpha := rc / (rc + dt)

	return &HPFProcessor{
		a1: -alpha,
		b0: (1 + alpha) / 2,
		b1: -(1 + alpha) / 2,
	}
}

// Process applies high-pass filter
func (h *HPFProcessor) Process(samples []float32) {
	for i := range samples {
		x0 := float64(samples[i])

		// IIR filter
		y0 := h.b0*x0 + h.b1*h.x1 - h.a1*h.y1

		// Update state
		h.x1 = x0
		h.y1 = y0

		samples[i] = float32(y0)
	}
}

// ==================== Feedback Suppression Processor ====================

// FeedbackSuppressionProcessor detects and suppresses acoustic feedback/howling
// This is crucial for preventing the "frequency wave" sound when speakers and mic are close
type FeedbackSuppressionProcessor struct {
	// Sample rate
	sampleRate int

	// FFT size for frequency analysis
	fftSize int

	// Maximum number of notch filters
	maxNotches int

	// Notch filter Q factor (higher = narrower notch)
	notchQ float64

	// Frequency range to monitor
	minFreqHz float64
	maxFreqHz float64

	// Detection threshold (0.0 to 1.0)
	detectionThreshold float64

	// Active notch filters
	notches []notchFilter

	// FFT buffers
	fftInput  []float64
	fftOutput []complex128

	// Hanning window
	window []float64

	// Frequency bins
	freqBins []float64

	// Running average spectrum for comparison
	avgSpectrum []float64
	avgAlpha    float64

	// Cooldown timer to prevent rapid notch changes
	cooldown int
}

// notchFilter represents a single notch filter
type notchFilter struct {
	freqHz     float64 // Center frequency
	enabled    bool    // Whether filter is active
	b0, b1, b2 float64 // Feedforward coefficients
	a1, a2     float64 // Feedback coefficients
	x1, x2     float64 // Input delay line
	y1, y2     float64 // Output delay line
}

// NewFeedbackSuppressionProcessor creates a new feedback suppression processor
func NewFeedbackSuppressionProcessor(sampleRate, maxNotches int, notchQ, minFreqHz, maxFreqHz float64) *FeedbackSuppressionProcessor {
	fftSize := 1024 // Good balance between frequency resolution and time resolution

	fbs := &FeedbackSuppressionProcessor{
		sampleRate:         sampleRate,
		fftSize:            fftSize,
		maxNotches:         maxNotches,
		notchQ:             notchQ,
		minFreqHz:          minFreqHz,
		maxFreqHz:          maxFreqHz,
		detectionThreshold: 0.7,
		notches:            make([]notchFilter, maxNotches),
		fftInput:           make([]float64, fftSize),
		fftOutput:          make([]complex128, fftSize/2+1),
		window:             makeHanningWindow(fftSize),
		freqBins:           make([]float64, fftSize/2+1),
		avgSpectrum:        make([]float64, fftSize/2+1),
		avgAlpha:           0.95,
		cooldown:           0,
	}

	// Initialize frequency bins
	for i := range fbs.freqBins {
		fbs.freqBins[i] = float64(i) * float64(sampleRate) / float64(fftSize)
	}

	return fbs
}

// Process applies feedback suppression to audio samples
func (fbs *FeedbackSuppressionProcessor) Process(samples []float32) {
	// Convert to float64 for processing
	floatSamples := make([]float64, len(samples))
	for i, s := range samples {
		floatSamples[i] = float64(s)
	}

	// Fill FFT input buffer with windowed samples
	for i := 0; i < fbs.fftSize && i < len(floatSamples); i++ {
		fbs.fftInput[i] = floatSamples[i] * fbs.window[i]
	}

	// Perform FFT (simplified - in production use a proper FFT library)
	spectrum := fbs.performFFT(fbs.fftInput)

	// Detect feedback frequencies
	feedbackFreqs := fbs.detectFeedback(spectrum)

	// Update notch filters
	if len(feedbackFreqs) > 0 && fbs.cooldown == 0 {
		fbs.updateNotches(feedbackFreqs)
		fbs.cooldown = 10 // Cooldown for 10 frames
	}

	if fbs.cooldown > 0 {
		fbs.cooldown--
	}

	// Apply notch filters to audio
	for i := range floatSamples {
		for j := range fbs.notches {
			if fbs.notches[j].enabled {
				floatSamples[i] = fbs.notches[j].process(floatSamples[i])
			}
		}
	}

	// Convert back to float32
	for i, s := range floatSamples {
		// Clamp to [-1.0, 1.0]
		if s > 1.0 {
			s = 1.0
		} else if s < -1.0 {
			s = -1.0
		}
		samples[i] = float32(s)
	}
}

// performFFT performs a simple DFT (in production, use FFTW or similar)
func (fbs *FeedbackSuppressionProcessor) performFFT(input []float64) []float64 {
	// Simplified FFT - returns magnitude spectrum
	n := len(input)
	spectrum := make([]float64, n/2+1)

	for k := 0; k <= n/2; k++ {
		var real, imag float64
		for t := 0; t < n; t++ {
			angle := -2.0 * math.Pi * float64(k) * float64(t) / float64(n)
			real += input[t] * math.Cos(angle)
			imag += input[t] * math.Sin(angle)
		}
		// Magnitude
		spectrum[k] = math.Sqrt(real*real+imag*imag) / float64(n)
	}

	return spectrum
}

// detectFeedback identifies feedback frequencies in the spectrum
func (fbs *FeedbackSuppressionProcessor) detectFeedback(spectrum []float64) []float64 {
	var feedbackFreqs []float64

	// Update running average
	for i := range spectrum {
		fbs.avgSpectrum[i] = fbs.avgAlpha*fbs.avgSpectrum[i] + (1-fbs.avgAlpha)*spectrum[i]
	}

	// Find peaks that exceed threshold
	for i := 1; i < len(spectrum)-1; i++ {
		freq := fbs.freqBins[i]

		// Skip frequencies outside our range
		if freq < fbs.minFreqHz || freq > fbs.maxFreqHz {
			continue
		}

		// Check if this is a peak
		if spectrum[i] > spectrum[i-1] && spectrum[i] > spectrum[i+1] {
			// Check if peak exceeds threshold relative to average
			if fbs.avgSpectrum[i] > 0 && spectrum[i]/fbs.avgSpectrum[i] > 1.0+fbs.detectionThreshold {
				feedbackFreqs = append(feedbackFreqs, freq)
			}
		}
	}

	return feedbackFreqs
}

// updateNotches updates the notch filter frequencies
func (fbs *FeedbackSuppressionProcessor) updateNotches(freqs []float64) {
	// Disable all existing notches first
	for i := range fbs.notches {
		fbs.notches[i].enabled = false
	}

	// Enable new notches for detected frequencies (up to maxNotches)
	for i, freq := range freqs {
		if i >= fbs.maxNotches {
			break
		}

		fbs.notches[i] = newNotchFilter(freq, fbs.sampleRate, fbs.notchQ)
		fbs.notches[i].enabled = true

		log.Printf("FeedbackSuppression: Added notch at %.0f Hz", freq)
	}
}

// newNotchFilter creates a new notch filter at the specified frequency
func newNotchFilter(freqHz float64, sampleRate int, q float64) notchFilter {
	// Calculate normalized frequency
	w0 := 2.0 * math.Pi * freqHz / float64(sampleRate)
	cosW0 := math.Cos(w0)
	sinW0 := math.Sin(w0)
	alpha := sinW0 / (2.0 * q)

	// Calculate coefficients (biquad notch filter)
	b0 := 1.0
	b1 := -2.0 * cosW0
	b2 := 1.0
	a0 := 1.0 + alpha
	a1 := -2.0 * cosW0
	a2 := 1.0 - alpha

	// Normalize
	b0 /= a0
	b1 /= a0
	b2 /= a0
	a1 /= a0
	a2 /= a0

	return notchFilter{
		freqHz: freqHz,
		b0:     b0,
		b1:     b1,
		b2:     b2,
		a1:     a1,
		a2:     a2,
		x1:     0,
		x2:     0,
		y1:     0,
		y2:     0,
	}
}

// process applies the notch filter to a single sample
func (n *notchFilter) process(x0 float64) float64 {
	// Biquad filter: y[n] = b0*x[n] + b1*x[n-1] + b2*x[n-2] - a1*y[n-1] - a2*y[n-2]
	y0 := n.b0*x0 + n.b1*n.x1 + n.b2*n.x2 - n.a1*n.y1 - n.a2*n.y2

	// Update delay lines
	n.x2 = n.x1
	n.x1 = x0
	n.y2 = n.y1
	n.y1 = y0

	return y0
}

// makeHanningWindow creates a Hanning window for FFT
func makeHanningWindow(size int) []float64 {
	window := make([]float64, size)
	for i := 0; i < size; i++ {
		window[i] = 0.5 * (1.0 - math.Cos(2.0*math.Pi*float64(i)/float64(size-1)))
	}
	return window
}

// ==================== Utility Functions ====================

// bytesToInt16 converts byte buffer to int16 samples (little-endian)
func bytesToInt16(data []byte) []int16 {
	if len(data)%2 != 0 {
		data = append(data, 0)
	}

	samples := make([]int16, len(data)/2)
	for i := 0; i < len(data); i += 2 {
		samples[i/2] = int16(data[i]) | int16(data[i+1])<<8
	}
	return samples
}

// int16ToBytes converts int16 samples to byte buffer (little-endian)
func int16ToBytes(samples []int16) []byte {
	data := make([]byte, len(samples)*2)
	for i, sample := range samples {
		data[i*2] = byte(sample)
		data[i*2+1] = byte(sample >> 8)
	}
	return data
}

// int16ToFloat32 converts int16 samples to float32 (-1.0 to 1.0)
func int16ToFloat32(samples []int16) []float32 {
	floatSamples := make([]float32, len(samples))
	for i, sample := range samples {
		floatSamples[i] = float32(sample) / 32768.0
	}
	return floatSamples
}

// float32ToInt16 converts float32 samples (-1.0 to 1.0) to int16
func float32ToInt16(samples []float32) []int16 {
	intSamples := make([]int16, len(samples))
	for i, sample := range samples {
		// Clamp to [-1.0, 1.0]
		if sample > 1.0 {
			sample = 1.0
		} else if sample < -1.0 {
			sample = -1.0
		}
		intSamples[i] = int16(sample * 32767.0)
	}
	return intSamples
}

// ==================== Recording Pipeline ====================

// RecordingPipeline provides audio processing for recording scenarios
type RecordingPipeline struct {
	processor *Processor
	enabled   bool
}

// NewRecordingPipeline creates a pipeline for recording with optional processing
func NewRecordingPipeline(enableProcessing bool) (*RecordingPipeline, error) {
	pipeline := &RecordingPipeline{
		enabled: enableProcessing,
	}

	if enableProcessing {
		config := DefaultConfig()
		// Disable echo cancellation for recording (no speaker reference)
		config.EchoCancellation = false
		processor, err := NewProcessor(config)
		if err != nil {
			log.Printf("RecordingPipeline: Failed to create processor, continuing without: %v", err)
			return pipeline, nil
		}
		pipeline.processor = processor
	}

	return pipeline, nil
}

// ProcessAudio processes audio for recording
func (rp *RecordingPipeline) ProcessAudio(pcmData []byte) ([]byte, error) {
	if !rp.enabled || rp.processor == nil {
		return pcmData, nil
	}

	return rp.processor.ProcessBufferInt16(pcmData)
}

// Close cleans up resources
func (rp *RecordingPipeline) Close() error {
	if rp.processor != nil {
		return rp.processor.Close()
	}
	return nil
}

// IsEnabled returns whether processing is enabled
func (rp *RecordingPipeline) IsEnabled() bool {
	return rp.enabled && rp.processor != nil
}

// ==================== Real-time Call Processor ====================

// CallProcessor provides audio processing optimized for real-time calls
// This is designed for server-side processing of WebRTC audio streams
type CallProcessor struct {
	processor *Processor
	enabled   bool

	// Far-end buffer for AEC synchronization
	farEndBuffer chan []float32
	bufferSize   int

	// Processing goroutine control
	stopCh chan struct{}
	wg     sync.WaitGroup
}

// NewCallProcessor creates a processor for real-time calls
func NewCallProcessor(enableProcessing bool) (*CallProcessor, error) {
	cp := &CallProcessor{
		enabled:      enableProcessing,
		farEndBuffer: make(chan []float32, 10),
		bufferSize:   480, // 10ms at 48kHz
		stopCh:       make(chan struct{}),
	}

	if enableProcessing {
		config := DefaultConfig()
		// Enable all features for real-time calls
		config.EchoCancellation = true
		config.NoiseSuppression = true
		config.AutomaticGainControl = true
		config.HighPassFilter = true

		// Optimize for low latency
		config.AECTailLengthMs = 50 // Shorter tail for lower latency
		config.AECStepSize = 0.3    // More conservative adaptation
		config.NSThresholdDb = -35  // Slightly less aggressive NS
		config.AGCAttackMs = 20     // Faster attack
		config.AGCReleaseMs = 200   // Faster release

		processor, err := NewProcessor(config)
		if err != nil {
			log.Printf("CallProcessor: Failed to create processor, continuing without: %v", err)
			return cp, nil
		}
		cp.processor = processor

		// Start processing goroutine
		cp.wg.Add(1)
		go cp.processingLoop()
	}

	return cp, nil
}

// processingLoop handles asynchronous AEC processing
func (cp *CallProcessor) processingLoop() {
	defer cp.wg.Done()

	for {
		select {
		case <-cp.stopCh:
			return
		case farEnd := <-cp.farEndBuffer:
			if cp.processor != nil {
				cp.processor.ProcessRender(farEnd)
			}
		}
	}
}

// ProcessFarEnd queues far-end audio for AEC processing
func (cp *CallProcessor) ProcessFarEnd(samples []float32) {
	if !cp.enabled || cp.processor == nil {
		return
	}

	// Make a copy to avoid data races
	copied := make([]float32, len(samples))
	copy(copied, samples)

	select {
	case cp.farEndBuffer <- copied:
		// Successfully queued
	default:
		// Buffer full, drop oldest
		select {
		case <-cp.farEndBuffer:
		default:
		}
		cp.farEndBuffer <- copied
	}
}

// ProcessNearEnd processes near-end audio with AEC
func (cp *CallProcessor) ProcessNearEnd(samples []float32) ([]float32, error) {
	if !cp.enabled || cp.processor == nil {
		return samples, nil
	}

	return cp.processor.ProcessFrame(samples)
}

// ProcessNearEndInt16 processes near-end audio in int16 format
func (cp *CallProcessor) ProcessNearEndInt16(buffer []byte) ([]byte, error) {
	if !cp.enabled || cp.processor == nil {
		return buffer, nil
	}

	return cp.processor.ProcessBufferInt16(buffer)
}

// ProcessAudioPacket processes an RTP audio packet payload for feedback suppression
// This is called for each audio packet flowing through the SFU
// Returns the processed payload (may be modified in-place or return new slice)
func (cp *CallProcessor) ProcessAudioPacket(payload []byte) []byte {
	if !cp.enabled || cp.processor == nil || len(payload) == 0 {
		return payload
	}

	// Process the payload through the audio processor
	// Note: For Opus packets, we would ideally decode to PCM, process, then re-encode
	// For now, we apply a simplified processing approach

	// Convert to int16 samples (Opus payload is typically already processed)
	samples := bytesToInt16(payload)

	// Convert to float32 for processing
	floatSamples := int16ToFloat32(samples)

	// Process through the pipeline (NS, AGC, HPF, Feedback Suppression)
	processed, err := cp.processor.ProcessFrame(floatSamples)
	if err != nil {
		// If processing fails, return original payload
		return payload
	}

	// Convert back to int16
	intSamples := float32ToInt16(processed)

	// Convert back to bytes
	return int16ToBytes(intSamples)
}

// Close cleans up resources

func (cp *CallProcessor) Close() error {
	close(cp.stopCh)
	cp.wg.Wait()

	if cp.processor != nil {
		return cp.processor.Close()
	}
	return nil
}

// IsEnabled returns whether processing is enabled
func (cp *CallProcessor) IsEnabled() bool {
	return cp.enabled && cp.processor != nil
}

// GetStats returns processing statistics
func (cp *CallProcessor) GetStats() map[string]interface{} {
	if !cp.enabled || cp.processor == nil {
		return map[string]interface{}{
			"enabled": false,
		}
	}

	return map[string]interface{}{
		"enabled":                true,
		"echo_cancellation":      cp.processor.config.EchoCancellation,
		"noise_suppression":      cp.processor.config.NoiseSuppression,
		"automatic_gain_control": cp.processor.config.AutomaticGainControl,
		"high_pass_filter":       cp.processor.config.HighPassFilter,
	}
}

// WaitForProcessing blocks until all pending processing is complete
// Useful for tests and graceful shutdown
func (cp *CallProcessor) WaitForProcessing(timeout time.Duration) bool {
	done := make(chan struct{})
	go func() {
		cp.wg.Wait()
		close(done)
	}()

	select {
	case <-done:
		return true
	case <-time.After(timeout):
		return false
	}
}
