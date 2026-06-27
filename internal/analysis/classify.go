// Package analysis classifies Starlink packet-loss events into one of four
// cause categories using RF signal quality and dish-reported alert flags.
package analysis

// The four recognized drop-cause categories. Prefix indicates the data source.
const (
	CauseRFBlockage = "[RF] Transient Physical Blockage / Canopy Handoff Failure"
	CauseRFInterfer = "[RF] Local Terrestrial EMI / Radar Interference"
	CauseDishSignal = "[dish] Signal Below Model / Possible Blockage"
	CauseCongestion = "[!] Downstream Network Pop / Carrier Congestion"
)

// DiagnoseParams carries the signal-quality inputs for drop cause classification.
type DiagnoseParams struct {
	BeaconSNR, BaselineSNR    *float64
	NoiseFloor, BaselineNoise *float64
	LowerSignalThanPredicted  bool
	IsSnrAboveNoiseFloor      bool
	SNRDelta                  float64
	NoiseDelta                float64
}

// DiagnoseCause classifies a packet-loss event into one of the four cause
// categories. Priority: RF blockage > EMI > dish signal alert > congestion.
func DiagnoseCause(p DiagnoseParams) string {
	snrDipped  := p.BeaconSNR != nil && p.BaselineSNR != nil && (*p.BeaconSNR < *p.BaselineSNR-p.SNRDelta)
	noiseSpike := p.NoiseFloor != nil && p.BaselineNoise != nil && (*p.NoiseFloor > *p.BaselineNoise+p.NoiseDelta)
	dishSignal := p.LowerSignalThanPredicted || !p.IsSnrAboveNoiseFloor
	switch {
	case snrDipped:
		return CauseRFBlockage
	case noiseSpike:
		return CauseRFInterfer
	case dishSignal:
		return CauseDishSignal
	default:
		return CauseCongestion
	}
}
