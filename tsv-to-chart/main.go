package main

import (
	"bufio"
	"flag"
	"log"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/wcharczuk/go-chart"
	"github.com/wcharczuk/go-chart/drawing"
)

func getColors(alpha uint8) []drawing.Color {
	return []drawing.Color{
		drawing.Color{R: 0, G: 116, B: 217, A: alpha},
		drawing.Color{R: 0, G: 217, B: 101, A: alpha},
		drawing.Color{R: 217, G: 0, B: 116, A: alpha},
		drawing.Color{R: 0, G: 217, B: 210, A: alpha},
		drawing.Color{R: 217, G: 101, B: 0, A: alpha},
		drawing.Color{R: 217, G: 210, B: 0, A: alpha},
		drawing.Color{R: 51, G: 51, B: 51, A: alpha},
		drawing.Color{R: 239, G: 239, B: 239, A: alpha},
	}
}

func main() {
	alpha := flag.Int("alpha", 255, "Alpha value for stroke color (0-255, default: 255)")
	outpath := flag.String("out", "out.png", "Path to output file (default: out.png)")
	timeseries := flag.Bool("timeseries", false, "Treat as timeseries data (default: false)")
	flag.Parse()

	colors := getColors(uint8(*alpha))
	series := []chart.Series{}

	paths := flag.Args()
	for i, path := range paths {
		log.Printf("loading %s", path)

		f, err := os.Open(path)
		if err != nil {
			log.Fatal(err)
		}

		xs := []float64{}
		ys := []float64{}

		s := bufio.NewScanner(f)
		for s.Scan() {
			line := s.Text()
			fields := strings.Split(line, "\t")

			x, err := strconv.ParseFloat(fields[0], 64)
			if err != nil {
				log.Fatal(err)
			}

			y, err := strconv.ParseFloat(fields[1], 64)
			if err != nil {
				log.Fatal(err)
			}

			xs = append(xs, x)
			ys = append(ys, y)
		}

		name := filepath.Base(path)
		color := colors[i%len(colors)]

		if *timeseries {
			txs := []time.Time{}
			for _, x := range xs {
				t := time.Unix(0, int64(x*1000*1000*1000))
				txs = append(txs, t)
			}
			series = append(series, chart.TimeSeries{
				Name:    name,
				XValues: txs,
				YValues: ys,
				Style: chart.Style{
					Show:        true,
					StrokeColor: color,
				},
			})
		} else {
			series = append(series, chart.ContinuousSeries{
				Name:    name,
				XValues: xs,
				YValues: ys,
				Style: chart.Style{
					Show:        true,
					StrokeColor: color,
				},
			})
		}
	}

	formatter := chart.FloatValueFormatter
	if *timeseries {
		formatter = chart.TimeMinuteValueFormatter
	}

	graph := chart.Chart{
		XAxis: chart.XAxis{
			Style: chart.Style{
				Show: true,
			},
			ValueFormatter: formatter,
		},
		YAxis: chart.YAxis{
			Style: chart.Style{
				Show: true,
			},
		},
		Series: series,
	}

	graph.Elements = []chart.Renderable{
		chart.LegendLeft(&graph),
	}

	out, err := os.Create(*outpath)
	if err != nil {
		log.Fatal(err)
	}

	log.Printf("rendering a chart to %s", *outpath)
	graph.Render(chart.PNG, out)

	log.Println("done")
}
