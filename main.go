package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"io/ioutil"

	"github.com/golang/geo/s2"
)

type Point struct {
	Lat float64
	Lng float64
}

type GeoJSONFeatureCollection struct {
	Type     string           `json:"type"`
	Features []GeoJSONFeature `json:"features"`
}

type GeoJSONFeature struct {
	Type       string                 `json:"type"`
	Properties map[string]interface{} `json:"properties"`
	Geometry   struct {
		Type        string         `json:"type"`
		Coordinates [][][2]float64 `json:"coordinates"`
	} `json:"geometry"`
}

func DecodeGeoJSON(enc []byte) (*GeoJSONFeatureCollection, error) {
	var fc GeoJSONFeatureCollection

	if err := json.Unmarshal(enc, &fc); err != nil {
		return nil, fmt.Errorf("json decode failed: %v", err)
	}

	if fc.Type != "FeatureCollection" {
		return nil, fmt.Errorf("GeoJSON document type unsupported: %v", fc.Type)
	}

	for i, feat := range fc.Features {
		if feat.Type != "Feature" {
			return nil, fmt.Errorf("GeoJSON feature %d unsupported type: %v", i, feat.Type)
		}
		if feat.Geometry.Type != "Polygon" {
			return nil, fmt.Errorf("GeoJSON feature %d unsupported geometry: %v", i, feat.Geometry.Type)
		}
	}

	return &fc, nil
}

// PointsToPolygon converts points to s2 polygon
func PointsToPolygon(points [][2]float64) *s2.Polygon {
	var pts []s2.Point
	for _, pt := range points {
		pts = append(pts, s2.PointFromLatLng(s2.LatLngFromDegrees(pt[1], pt[0])))
	}
	loop := s2.LoopFromPoints(pts)

	return s2.PolygonFromLoops([]*s2.Loop{loop})
}

func CoverPolygon(p *s2.Polygon, minLevel, maxLevel int, interior bool) []s2.CellID {
	rc := &s2.RegionCoverer{MaxLevel: maxLevel, MinLevel: minLevel, MaxCells: 100000}
	r := s2.Region(p)

	var covering s2.CellUnion
	if interior {
		covering = rc.InteriorCovering(r)
	} else {
		covering = rc.Covering(r)
	}

	return []s2.CellID(covering)
}

func CellsToGeoJSONFeatureCollection(cellIDs []s2.CellID) *GeoJSONFeatureCollection {
	fc := GeoJSONFeatureCollection{
		Type:     "FeatureCollection",
		Features: make([]GeoJSONFeature, len(cellIDs)),
	}

	for i, cellID := range cellIDs {
		cellToken := cellID.ToToken()
		cell := s2.CellFromCellID(s2.CellIDFromToken(cellToken))

		fc.Features[i].Type = "Feature"

		fc.Features[i].Properties = map[string]interface{}{
			"entity_id": cellToken,
			"labels": map[string]string{
				"s2CellToken": cellToken,
				"s2Level":     fmt.Sprintf("%d", cell.Level()),
			},
		}

		fc.Features[i].Geometry.Type = "Polygon"

		// have to reverse the order of lat/lng per GeoJSON
		var coords [][2]float64
		for _, point := range EdgesOfCell(cell) {
			coords = append(coords, [2]float64{point[1], point[0]})
		}

		fc.Features[i].Geometry.Coordinates = [][][2]float64{coords}
	}

	return &fc
}

func EdgesOfCell(c s2.Cell) [][2]float64 {
	var edges [][2]float64
	for i := 0; i < 4; i++ {
		latLng := s2.LatLngFromPoint(c.Vertex(i))
		edges = append(edges, [2]float64{latLng.Lat.Degrees(), latLng.Lng.Degrees()})
	}

	// need to close the loop
	edges = append(edges, edges[0])

	return edges
}

func main() {
	var flagInput string
	flag.StringVar(&flagInput, "input", "", "path to file containing GeoJSON FeatureCollection")

	var flagMerge bool
	flag.BoolVar(&flagMerge, "merge", false, "if true, merge output into input GeoJSON")

	var flagInterior bool
	flag.BoolVar(&flagMerge, "interior", false, "if true, restrict covering to fully-contained cells")

	var flagMin, flagMax int
	flag.IntVar(&flagMin, "min", 1, "min level of S2 cells desired")
	flag.IntVar(&flagMax, "max", 30, "max level of S2 cells desired")

	flag.Parse()

	raw, err := ioutil.ReadFile(flagInput)
	if err != nil {
		panic(fmt.Sprintf("failed reading input file: %v", err))
	}

	inputFC, err := DecodeGeoJSON(raw)
	if err != nil {
		panic(fmt.Sprintf("failed decoding GeoJSON: %v", err))
	}

	if len(inputFC.Features) != 1 {
		panic("unable to handle input FeatureCollection; must provide one Feature")
	}

	// only handling the first feature for now
	poly := PointsToPolygon(inputFC.Features[0].Geometry.Coordinates[0])
	cellIDs := CoverPolygon(poly, flagMin, flagMax, flagInterior)
	cellFC := CellsToGeoJSONFeatureCollection(cellIDs)

	if flagMerge {
		cellFC.Features = append(inputFC.Features, cellFC.Features...)
	}

	enc, err := json.Marshal(cellFC)
	if err != nil {
		panic(fmt.Sprintf("failed encoding output FeatureCollection: %v", err))
	}

	fmt.Printf(string(enc))
}
