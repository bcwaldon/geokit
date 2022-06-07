package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io/ioutil"

	"github.com/golang/geo/s2"
	"googlemaps.github.io/maps"
)

// belongs to brian.waldon@sustglobal.com
const GOOGLE_MAPS_API_KEY = "AIzaSyDigtazqoqoVnLoTn1MnUf5cXMZn6i6XhU"

type GeoJSONFeatureCollection struct {
	Type     string           `json:"type"`
	Features []GeoJSONFeature `json:"features"`
}

type GeoJSONFeature struct {
	Type       string                 `json:"type"`
	Properties map[string]interface{} `json:"properties"`
	Geometry   GeoJSONGeometry        `json:"geometry"`
}

type GeoJSONGeometry struct {
	Type        string      `json:"type"`
	Coordinates interface{} `json:"coordinates"`
}

func (f *GeoJSONFeature) TypedGeometry() (interface{}, error) {
	var geo interface{}
	switch f.Geometry.Type {
	case "Point":
		geo = new(GeoJSONPointGeometry)
	case "Polygon":
		geo = new(GeoJSONPolygonGeometry)
	default:
		return nil, fmt.Errorf("unsupported geometry %q", f.Geometry.Type)
	}

	enc, _ := json.Marshal(f.Geometry)
	if err := json.Unmarshal(enc, geo); err != nil {
		return nil, fmt.Errorf("failed decoding typed geometry: %v", err)
	}

	return geo, nil
}

type GeoJSONPolygonGeometry struct {
	Type        string         `json:"type"`
	Coordinates [][][2]float64 `json:"coordinates"`
}

type GeoJSONPointGeometry struct {
	Type        string     `json:"type"`
	Coordinates [2]float64 `json:"coordinates"`
}

func DecodeGeoJSONFeatures(enc []byte) ([]GeoJSONFeature, error) {
	var fc GeoJSONFeatureCollection

	if err := json.Unmarshal(enc, &fc); err != nil {
		return nil, fmt.Errorf("json decode failed: %v", err)
	}

	if fc.Type != "FeatureCollection" {
		return nil, fmt.Errorf("GeoJSON document type unsupported: %v", fc.Type)
	}

	return fc.Features, nil
}

func GeoJSONPolygonToS2Polygon(poly *GeoJSONPolygonGeometry) *s2.Polygon {
	var pts []s2.Point
	for _, pt := range poly.Coordinates[0] {
		pts = append(pts, s2.PointFromLatLng(s2.LatLngFromDegrees(pt[1], pt[0])))
	}
	loop := s2.LoopFromPoints(pts)
	return s2.PolygonFromLoops([]*s2.Loop{loop})
}

func Cover(r s2.Region, minLevel, maxLevel int, interior bool) []s2.CellID {
	rc := &s2.RegionCoverer{MaxLevel: maxLevel, MinLevel: minLevel, MaxCells: 100000}

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

func Geocode(addr string) (*GeoJSONGeometry, error) {
	cl, err := maps.NewClient(maps.WithAPIKey(GOOGLE_MAPS_API_KEY))
	if err != nil {
		return nil, err
	}

	req := maps.GeocodingRequest{
		Address: addr,
	}
	results, err := cl.Geocode(context.Background(), &req)
	if err != nil {
		return nil, err
	}

	if len(results) != 1 {
		return nil, fmt.Errorf("expected one results from Geocoding API, received %d", len(results))
	}

	geo := GeoJSONGeometry{
		Type: "Point",
		Coordinates: [2]float64{
			results[0].Geometry.Location.Lng,
			results[0].Geometry.Location.Lat,
		},
	}

	return &geo, nil
}

func main() {
	var flagAddress string
	flag.StringVar(&flagAddress, "address", "", "address that should be geocoded to a point")

	var flagGeoJSON string
	flag.StringVar(&flagGeoJSON, "geojson", "", "path to file containing GeoJSON FeatureCollection")

	var flagMerge bool
	flag.BoolVar(&flagMerge, "merge", false, "if true, merge output into input GeoJSON")

	var flagInterior bool
	flag.BoolVar(&flagInterior, "interior", false, "if true, restrict covering to fully-contained cells")

	var flagMin, flagMax int
	flag.IntVar(&flagMin, "min", 1, "min level of S2 cells desired")
	flag.IntVar(&flagMax, "max", 30, "max level of S2 cells desired")

	flag.Parse()

	var inputFeatures []GeoJSONFeature

	if flagAddress != "" {
		geo, err := Geocode(flagAddress)
		if err != nil {
			panic(fmt.Sprintf("failed geocoding: %v", err))
		}

		inputFeatures = []GeoJSONFeature{
			GeoJSONFeature{
				Type:     "Feature",
				Geometry: *geo,
				Properties: map[string]interface{}{
					"address": flagAddress,
				},
			},
		}

	} else if flagGeoJSON != "" {
		raw, err := ioutil.ReadFile(flagGeoJSON)
		if err != nil {
			panic(fmt.Sprintf("failed reading input file: %v", err))
		}

		inputFeatures, err = DecodeGeoJSONFeatures(raw)
		if err != nil {
			panic(fmt.Sprintf("failed decoding GeoJSON: %v", err))
		}

	} else {
		panic("must only provide one of --address or --geojson")
	}

	var s2CellIDs []s2.CellID

	for _, feat := range inputFeatures {
		geo, err := feat.TypedGeometry()
		if err != nil {
			panic(err)
		}

		switch geo.(type) {
		case *GeoJSONPolygonGeometry:
			poly := geo.(*GeoJSONPolygonGeometry)
			s2Poly := GeoJSONPolygonToS2Polygon(poly)
			s2CellIDs = append(s2CellIDs, Cover(s2.Region(s2Poly), flagMin, flagMax, flagInterior)...)
		case *GeoJSONPointGeometry:
			pt := geo.(*GeoJSONPointGeometry)
			s2Point := s2.PointFromLatLng(s2.LatLngFromDegrees(pt.Coordinates[1], pt.Coordinates[0]))
			s2CellIDs = append(s2CellIDs, Cover(s2.Region(s2Point), flagMin, flagMax, flagInterior)...)
		default:
			panic("unable to handle geometry")
		}
	}

	s2CellFC := CellsToGeoJSONFeatureCollection(s2CellIDs)

	if flagMerge {
		s2CellFC.Features = append(inputFeatures, s2CellFC.Features...)
	}

	enc, err := json.Marshal(s2CellFC)
	if err != nil {
		panic(fmt.Sprintf("failed encoding output FeatureCollection: %v", err))
	}

	fmt.Printf(string(enc))
}
