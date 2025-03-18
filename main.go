package main

import (
	"encoding/binary"
	"fmt"
	"github.com/fsnotify/fsnotify"
	"github.com/spf13/viper"
	"log"
	"math"
	"slices"
	"strings"
	"tinygo.org/x/bluetooth"
)

type SensorData struct {
	MacAddress  string
	Name        string
	BatLevel    uint16
	RSSI        int16
	Uptime      uint32
	Temperature float64
	Humidity    float64
}

var (
	insideSensor   string
	insideTempCal  float64
	insideHumCal   float64
	outsideSensor  string
	outsideTempCal float64
	outsideHumCal  float64
)

func main() {
	viper.SetConfigName("config")
	viper.SetConfigType("json")
	viper.AddConfigPath(".")
	viper.OnConfigChange(func(e fsnotify.Event) {
		log.Println("Config file changed:", e.Name)
		readConfig()
	})
	viper.WatchConfig()
	readConfig()
	adapter := bluetooth.DefaultAdapter
	err := adapter.Enable()
	if err != nil {
		panic("failed to enable BLE adapter")
	}

	err = adapter.Scan(onScan)
	if err != nil {
		panic("failed to register scan callback")
	}
}

func readConfig() {
	err := viper.ReadInConfig()
	if err != nil {
		log.Fatalf("Fatal error reading config file: %s \n", err)
	}
	insideSensor = viper.GetString("inside.mac")
	insideTempCal = viper.GetFloat64("inside.temperature-calibration")
	insideHumCal = viper.GetFloat64("inside.humidity-calibration")
	outsideSensor = viper.GetString("outside.mac")
	outsideTempCal = viper.GetFloat64("outside.temperature-calibration")
	outsideHumCal = viper.GetFloat64("outside.humidity-calibration")
	log.Printf("Inside sensor:  MAC %s - Temp cal = %.2f - Humidity cal = %.2f", insideSensor, insideTempCal, insideHumCal)
	log.Printf("Outside sensor: MAC %s - Temp cal = %.2f - Humidity cal = %.2f", outsideSensor, outsideTempCal, outsideHumCal)
	if len(insideSensor) != 17 || len(outsideSensor) != 17 {
		log.Fatal("Invalid MAC address! Must be 17 characters long.")
	}
}

func onScan(_ *bluetooth.Adapter, scanResult bluetooth.ScanResult) {
	if scanResult.LocalName() == "ThermoBeacon" {
		processAdvertisement(scanResult)
	}
}

func processAdvertisement(scanResult bluetooth.ScanResult) {
	payload := scanResult.AdvertisementPayload.ManufacturerData()[0].Data
	if len(payload) == 18 {
		sensorData := parseWS02Data(payload, scanResult.RSSI)
		if sensorData.Name != "" {
			log.Printf("%8s Temp: %.1f°C - Hum: %.1f%% - Bat: %d - RSSI: %d - Uptime: %s",
				sensorData.Name, sensorData.Temperature, sensorData.Humidity, sensorData.BatLevel,
				sensorData.RSSI, formatUptime(sensorData.Uptime))
		}
	}
}

func parseWS02Data(payload []byte, rssi int16) *SensorData {
	// The WS02 advertisement contains temperature and humidity in specific locations.
	// The mac address starts at offset 2 and the 16-bit value of the battery level starts at offset 8.
	// The temperature is a 16-bit value starting at offset 10 and humidity is a 16-bit value starting at offset 12.
	// The uptime in seconds since the last reset is a 32-bit value starting at offset 14.
	const macOffset = 2
	const batOffset = 8
	const tempOffset = 10
	const humidityOffset = 12
	const uptimeOffset = 14

	macAdr := ""
	for _, c := range slices.Backward(payload[macOffset : macOffset+6]) {
		macAdr = macAdr + fmt.Sprintf("%02X:", c)
	}
	macAdr = strings.TrimSuffix(macAdr, ":")
	name := ""
	tempCal := 0.0
	humCal := 0.0
	if macAdr == insideSensor {
		name = "Inside"
		tempCal = insideTempCal
		humCal = insideHumCal
	} else if macAdr == outsideSensor {
		name = "Outside"
		tempCal = outsideTempCal
		humCal = outsideHumCal
	}
	batLevel := binary.LittleEndian.Uint16(payload[batOffset : batOffset+2])
	uptime := binary.LittleEndian.Uint32(payload[uptimeOffset : uptimeOffset+4])

	temperatureInt := binary.LittleEndian.Uint16(payload[tempOffset : tempOffset+2])
	temperatureRaw := float64(temperatureInt) / 16.0
	if temperatureRaw > 4000 {
		temperatureRaw -= 4096
	}
	temperature := temperatureRaw + tempCal

	humidityInt := binary.LittleEndian.Uint16(payload[humidityOffset : humidityOffset+2])
	humidityRaw := float64(humidityInt) / 16.0
	if humidityRaw > 4000 {
		humidityRaw -= 4096
	}
	humidity := humidityRaw + humCal

	return &SensorData{
		MacAddress:  macAdr,
		Name:        name,
		BatLevel:    batLevel,
		RSSI:        rssi,
		Uptime:      uptime,
		Temperature: math.Round(temperature*10) / 10,
		Humidity:    math.Round(humidity*10) / 10,
	}
}

func formatUptime(seconds uint32) string {
	// Calculate the number of days, hours, and minutes
	days := seconds / (24 * 3600)
	hours := (seconds % (24 * 3600)) / 3600
	minutes := (seconds % 3600) / 60

	// Create the formatted string
	return fmt.Sprintf("%dd %dh %dm", days, hours, minutes)
}
