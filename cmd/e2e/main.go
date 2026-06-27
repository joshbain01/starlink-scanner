package main

import (
	"context"
	"fmt"
	"log"
	"sync"
	"time"

	"pp-starlink/internal/db"
	"pp-starlink/internal/ping"
	"pp-starlink/internal/starlink"
)

func main() {
	d, err := db.Open("/data/starlink_telemetry.db")
	if err != nil { log.Fatal("db:", err) }
	defer d.Close()

	sc, err := starlink.Dial("192.168.100.1:9200")
	if err != nil { log.Fatal("dial:", err) }
	defer sc.Close()

	targets := [3]string{"192.168.100.1", "", "1.1.1.1"}
	var (
		mu    sync.Mutex
		pings [3]ping.Result
		wg    sync.WaitGroup
	)
	for i, t := range targets {
		if t == "" { continue }
		wg.Add(1)
		go func(i int, t string) {
			defer wg.Done()
			r, err := ping.Run(t)
			if err != nil { log.Printf("ping %s: %v", t, err); return }
			mu.Lock(); pings[i] = r; mu.Unlock()
		}(i, t)
	}

	fmt.Println("→ GetStatus via gRPC...")
	status, err := sc.GetStatus(context.Background())
	if err != nil { log.Printf("grpc: %v", err) }
	wg.Wait()

	fmt.Printf("  uptime=%ds obstruction=%.4f\n", status.UptimeS, status.ObstructionFraction)
	fmt.Printf("  alerts: shutdown=%v throttle=%v sloweth=%v motors=%v mast=%v\n",
		status.Alerts.ThermalShutdown, status.Alerts.ThermalThrottle,
		status.Alerts.SlowEthernet, status.Alerts.MotorsStuck, status.Alerts.MastNotNearVertical)
	fmt.Printf("  gateway jitter=%.2fms  public loss=%.1f%%\n",
		pings[0].JitterMs, pings[2].PacketLoss*100)

	sample := db.NetworkSample{
		Timestamp:           time.Now(),
		UptimeS:             status.UptimeS,
		ObstructionFraction: status.ObstructionFraction,
		ThermalShutdown:     status.Alerts.ThermalShutdown,
		ThermalThrottle:     status.Alerts.ThermalThrottle,
		SlowEthernet:        status.Alerts.SlowEthernet,
		GatewayJitterMs:     pings[0].JitterMs,
		POPJitterMs:         pings[1].JitterMs,
		PublicPacketLoss:    pings[2].PacketLoss,
	}
	if err := d.WriteNetwork(sample); err != nil { log.Fatal("write:", err) }
	fmt.Println("→ row written OK")

	// inject a synthetic drop event so insights has something to query
	drop := db.NetworkSample{
		Timestamp:        time.Now().Add(-30 * time.Second),
		UptimeS:          status.UptimeS,
		PublicPacketLoss: 0.20,
	}
	if err := d.WriteNetwork(drop); err != nil { log.Fatal("write drop:", err) }
	fmt.Println("→ synthetic drop row written")
}
