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

// DiagnosticTrace carries a classification result plus an audit trail of
// every classifier that was evaluated. Reserved for future use; existing
// callers should continue using DiagnoseCause.
type DiagnosticTrace struct {
	Cause     string
	Matched   string
	Evaluated []string
}

// Classifier is the single-method interface used by the chain-of-responsibility.
type Classifier interface {
	Classify(p DiagnoseParams) (cause string, matched bool)
}

// RFBlockageClassifier fires when beacon SNR dips below the 24-hour baseline.
type RFBlockageClassifier struct{}

func (RFBlockageClassifier) Classify(p DiagnoseParams) (string, bool) {
	if p.BeaconSNR != nil && p.BaselineSNR != nil && (*p.BeaconSNR < *p.BaselineSNR-p.SNRDelta) {
		return CauseRFBlockage, true
	}
	return "", false
}

// EMIClassifier fires when the noise floor spikes above the 24-hour baseline.
type EMIClassifier struct{}

func (EMIClassifier) Classify(p DiagnoseParams) (string, bool) {
	if p.NoiseFloor != nil && p.BaselineNoise != nil && (*p.NoiseFloor > *p.BaselineNoise+p.NoiseDelta) {
		return CauseRFInterfer, true
	}
	return "", false
}

// DishSignalClassifier fires when the dish reports a below-model signal condition.
type DishSignalClassifier struct{}

func (DishSignalClassifier) Classify(p DiagnoseParams) (string, bool) {
	if p.LowerSignalThanPredicted || !p.IsSnrAboveNoiseFloor {
		return CauseDishSignal, true
	}
	return "", false
}

// CongestionClassifier is the fallback; it always matches.
type CongestionClassifier struct{}

func (CongestionClassifier) Classify(_ DiagnoseParams) (string, bool) {
	return CauseCongestion, true
}

// ChainClassifier tries each Classifier in order and returns the first match.
type ChainClassifier struct {
	classifiers []Classifier
}

func (c ChainClassifier) Classify(p DiagnoseParams) (string, bool) {
	for _, cl := range c.classifiers {
		if cause, ok := cl.Classify(p); ok {
			return cause, true
		}
	}
	return "", false
}

// defaultChain is the canonical classifier used by DiagnoseCause and DiagnoseWithTrace.
var defaultChain = ChainClassifier{
	classifiers: []Classifier{
		RFBlockageClassifier{},
		EMIClassifier{},
		DishSignalClassifier{},
		CongestionClassifier{},
	},
}

// DiagnoseCause classifies a packet-loss event into one of the four cause
// categories. Priority: RF blockage > EMI > dish signal alert > congestion.
// Signature and behaviour are unchanged.
func DiagnoseCause(p DiagnoseParams) string {
	cause, _ := defaultChain.Classify(p)
	return cause
}

// DiagnoseWithTrace returns the classification result together with an audit
// trail showing every classifier that was evaluated before a match was found.
func DiagnoseWithTrace(p DiagnoseParams) DiagnosticTrace {
	evaluated := make([]string, 0, len(defaultChain.classifiers))
	for _, cl := range defaultChain.classifiers {
		name := classifierName(cl)
		evaluated = append(evaluated, name)
		if cause, ok := cl.Classify(p); ok {
			return DiagnosticTrace{
				Cause:     cause,
				Matched:   name,
				Evaluated: evaluated,
			}
		}
	}
	return DiagnosticTrace{Evaluated: evaluated}
}

func classifierName(c Classifier) string {
	switch c.(type) {
	case RFBlockageClassifier:
		return "RFBlockageClassifier"
	case EMIClassifier:
		return "EMIClassifier"
	case DishSignalClassifier:
		return "DishSignalClassifier"
	case CongestionClassifier:
		return "CongestionClassifier"
	default:
		return "UnknownClassifier"
	}
}
