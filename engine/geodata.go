package engine

import (
	"context"
	"errors"
	"fmt"
	"unicode/utf8"

	"google.golang.org/protobuf/encoding/protowire"
)

type geoMessage uint8

const (
	geoSiteList geoMessage = iota
	geoIPList
	geoSite
	geoIP
	geoDomain
	geoCIDR
	geoAttribute
)

type geoValidation struct{ entries, records int }

// ValidateGeoData checks Xray's GeoSiteList/GeoIPList wire format before an
// update replaces routing data. Unknown fields remain forward compatible.
// Schema: https://github.com/XTLS/Xray-core/blob/main/common/geodata/geodat.proto
func ValidateGeoData(ctx context.Context, name string, data []byte) error {
	var kind geoMessage
	switch name {
	case "geosite.dat":
		kind = geoSiteList
	case "geoip.dat":
		kind = geoIPList
	default:
		return fmt.Errorf("unsupported geo data file %q", name)
	}
	stats := &geoValidation{}
	if err := validateGeoMessage(ctx, kind, data, stats); err != nil {
		return fmt.Errorf("invalid %s: %w", name, err)
	}
	if stats.entries == 0 || stats.records == 0 {
		return fmt.Errorf("%s contains no routing data", name)
	}
	return nil
}

func validateGeoMessage(
	ctx context.Context,
	kind geoMessage,
	data []byte,
	stats *geoValidation,
) error {
	var text string
	var ip []byte
	var number uint64
	for len(data) > 0 {
		if err := ctx.Err(); err != nil {
			return err
		}
		field, wire, tagSize := protowire.ConsumeTag(data)
		if tagSize < 0 {
			return protowire.ParseError(tagSize)
		}
		data = data[tagSize:]
		valueSize := protowire.ConsumeFieldValue(field, wire, data)
		if valueSize < 0 {
			return protowire.ParseError(valueSize)
		}
		value := data[:valueSize]
		data = data[valueSize:]
		var bytes []byte
		var integer uint64
		if wire == protowire.BytesType {
			bytes, _ = protowire.ConsumeBytes(value)
		}
		if wire == protowire.VarintType {
			integer, _ = protowire.ConsumeVarint(value)
		}
		expect := func(want protowire.Type) error {
			if wire != want {
				return fmt.Errorf("field %d has invalid wire type", field)
			}
			return nil
		}
		switch kind {
		case geoSiteList, geoIPList:
			if field != 1 {
				continue
			}
			if err := expect(protowire.BytesType); err != nil {
				return err
			}
			child := geoSite
			if kind == geoIPList {
				child = geoIP
			}
			if err := validateGeoMessage(ctx, child, bytes, stats); err != nil {
				return err
			}
			stats.entries++
		case geoSite, geoIP:
			switch field {
			case 1:
				if err := expect(protowire.BytesType); err != nil {
					return err
				}
				text = string(bytes)
			case 2:
				if err := expect(protowire.BytesType); err != nil {
					return err
				}
				child := geoDomain
				if kind == geoIP {
					child = geoCIDR
				}
				if err := validateGeoMessage(ctx, child, bytes, stats); err != nil {
					return err
				}
				stats.records++
			case 3:
				if kind == geoIP {
					if err := expect(protowire.VarintType); err != nil {
						return err
					}
				}
			}
		case geoDomain:
			switch field {
			case 1:
				if err := expect(protowire.VarintType); err != nil {
					return err
				}
				number = integer
			case 2:
				if err := expect(protowire.BytesType); err != nil {
					return err
				}
				text = string(bytes)
			case 3:
				if err := expect(protowire.BytesType); err != nil {
					return err
				}
				if err := validateGeoMessage(ctx, geoAttribute, bytes, stats); err != nil {
					return err
				}
			}
		case geoCIDR:
			switch field {
			case 1:
				if err := expect(protowire.BytesType); err != nil {
					return err
				}
				ip = bytes
			case 2:
				if err := expect(protowire.VarintType); err != nil {
					return err
				}
				number = integer
			}
		case geoAttribute:
			switch field {
			case 1:
				if err := expect(protowire.BytesType); err != nil {
					return err
				}
				text = string(bytes)
			case 2, 3:
				if err := expect(protowire.VarintType); err != nil {
					return err
				}
			}
		}
	}
	switch kind {
	case geoSite, geoIP, geoDomain, geoAttribute:
		if text == "" || !utf8.ValidString(text) {
			return errors.New("missing or invalid geo data text")
		}
	case geoCIDR:
		if (len(ip) != 4 && len(ip) != 16) || number > uint64(len(ip)*8) {
			return errors.New("invalid geo data CIDR")
		}
	}
	if kind == geoDomain && number > 3 {
		return errors.New("invalid geo data domain type")
	}
	return nil
}
