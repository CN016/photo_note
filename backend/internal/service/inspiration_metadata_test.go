package service

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"image"
	"image/color"
	"image/png"
	"io"
	"math"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/photo-nini/backend/internal/model"
)

func TestParseExifToolMetadataMapsKnownPropertiesAndKeepsNamespaces(t *testing.T) {
	output := []byte(`[{"SourceFile":"-","EXIF:ImageWidth":1496,"EXIF:ImageHeight":942,"EXIF:XResolution":96,"EXIF:YResolution":96,"EXIF:ResolutionUnit":2,"EXIF:BitsPerSample":[8,8,8],"EXIF:Make":"Canon","EXIF:DateTimeOriginal":"2026:04:15 09:06:00","EXIF:ApertureValue":2,"EXIF:MaxApertureValue":1,"EXIF:ShutterSpeedValue":7,"EXIF:ISO":500,"EXIF:Flash":9,"EXIF:FlashEnergy":2.5,"EXIF:SerialNumber":"serial-123","GPS:GPSLatitude":31.2,"GPS:GPSLongitude":121.5,"IPTC:Keywords":["street","night"],"XMP:CustomField":"value"}]`)

	metadata, err := ParseExifToolMetadata(output)
	if err != nil {
		t.Fatalf("ParseExifToolMetadata() error = %v", err)
	}
	if metadata.ImageWidth == nil || *metadata.ImageWidth != 1496 || metadata.ImageHeight == nil || *metadata.ImageHeight != 942 {
		t.Fatalf("unexpected dimensions: width=%v height=%v", metadata.ImageWidth, metadata.ImageHeight)
	}
	if metadata.HorizontalResolutionDPI == nil || *metadata.HorizontalResolutionDPI != 96 {
		t.Fatalf("unexpected resolution: %v", metadata.HorizontalResolutionDPI)
	}
	if metadata.BitDepth == nil || *metadata.BitDepth != 24 {
		t.Fatalf("unexpected bit depth: %v", metadata.BitDepth)
	}
	if metadata.DateTimeOriginal == nil || *metadata.DateTimeOriginal != "2026:04:15 09:06:00" {
		t.Fatalf("raw capture date was not preserved: %v", metadata.DateTimeOriginal)
	}
	if metadata.ApertureFNumber == nil || *metadata.ApertureFNumber != 2 || metadata.MaximumApertureFNumber == nil || math.Abs(*metadata.MaximumApertureFNumber-math.Sqrt(2)) > 0.0001 {
		t.Fatalf("APEX aperture values were not normalized to F-numbers: aperture=%v maximum=%v", metadata.ApertureFNumber, metadata.MaximumApertureFNumber)
	}
	if metadata.ExposureTimeSeconds == nil || math.Abs(*metadata.ExposureTimeSeconds-1.0/128.0) > 0.0001 {
		t.Fatalf("APEX shutter speed was not normalized to seconds: %v", metadata.ExposureTimeSeconds)
	}
	if metadata.CameraSerialNumber == nil || *metadata.CameraSerialNumber != "serial-123" || metadata.FlashFired == nil || !*metadata.FlashFired {
		t.Fatalf("camera serial/flash property missing: serial=%v flash=%v", metadata.CameraSerialNumber, metadata.FlashFired)
	}
	if metadata.FlashEnergyBCPS == nil || *metadata.FlashEnergyBCPS != 2.5 {
		t.Fatalf("flash energy should use the EXIF BCPS unit: %v", metadata.FlashEnergyBCPS)
	}
	if metadata.Tags == nil || len(metadata.Tags) != 2 {
		t.Fatalf("unexpected keywords: %#v", metadata.Tags)
	}
	var extra map[string]json.RawMessage
	if err := json.Unmarshal(metadata.Extra, &extra); err != nil {
		t.Fatalf("extra JSON invalid: %v", err)
	}
	if string(extra["XMP:CustomField"]) != `"value"` {
		t.Fatalf("namespaced XMP field missing: %s", metadata.Extra)
	}
	if _, exists := extra["File:SourceFile"]; exists {
		t.Fatal("ExifTool input identifier must not be persisted as a tag")
	}
}

func TestParseExifToolMetadataHandlesEmptyGPSAndCapsExtraTags(t *testing.T) {
	large := strings.Repeat("x", maxImageMetadataExtraBytes+32)
	encoded, err := json.Marshal([]map[string]any{{
		"EXIF:Make": "Nikon", "XMP:Large": large, "XMP:Small": "keep-me",
		"File:FileName": `C:\\private\\camera.jpg`, "File:FileCreateDate": "2026:04:15 09:07:00",
	}})
	if err != nil {
		t.Fatal(err)
	}
	metadata, err := ParseExifToolMetadata(encoded)
	if err != nil {
		t.Fatalf("ParseExifToolMetadata() error = %v", err)
	}
	if metadata.CameraMake == nil || *metadata.CameraMake != "Nikon" {
		t.Fatalf("known property lost when extras exceeded limit: %v", metadata.CameraMake)
	}
	if metadata.GPSLatitude != nil || metadata.GPSLongitude != nil {
		t.Fatalf("GPS should remain empty: latitude=%v longitude=%v", metadata.GPSLatitude, metadata.GPSLongitude)
	}
	var extra map[string]string
	if err := json.Unmarshal(metadata.Extra, &extra); err != nil {
		t.Fatalf("extra JSON invalid: %v", err)
	}
	if extra["XMP:Small"] != "keep-me" || len(extra) != 1 {
		t.Fatalf("oversized and local file properties should be omitted while fitting tags remain: %#v", extra)
	}
}

func TestParseExifToolMetadataReadsJFIFResolutionUnits(t *testing.T) {
	metadata, err := ParseExifToolMetadata([]byte(`[{"JFIF:XResolution":96,"JFIF:YResolution":72,"JFIF:ResolutionUnit":1}]`))
	if err != nil {
		t.Fatalf("ParseExifToolMetadata() error = %v", err)
	}
	if metadata.HorizontalResolutionDPI == nil || *metadata.HorizontalResolutionDPI != 96 || metadata.VerticalResolutionDPI == nil || *metadata.VerticalResolutionDPI != 72 {
		t.Fatalf("JFIF inch resolution was not preserved as DPI: horizontal=%v vertical=%v", metadata.HorizontalResolutionDPI, metadata.VerticalResolutionDPI)
	}
}

func TestParseExifToolMetadataHandlesUnassignedGroupsAndDropsLocalFileProperties(t *testing.T) {
	output := []byte(`[{"SourceFile":"-",":ImageWidth":356,":ImageHeight":554,":BitDepth":8,":ColorType":2,":ProfileDescription":"sRGB IEC61966-2.1",":Compression":6,":FileModifyDate":"2026:10:16 15:39:19+00:00",":FileAccessDate":"2026:10:16 15:39:19+00:00",":FileInodeChangeDate":"2026:10:16 15:39:19+00:00",":FilePermissions":10600,":FileType":"PNG",":ImageSize":"356 554"}]`)
	metadata, err := ParseExifToolMetadata(output)
	if err != nil {
		t.Fatalf("ParseExifToolMetadata() error = %v", err)
	}
	if metadata.ImageWidth == nil || *metadata.ImageWidth != 356 || metadata.ImageHeight == nil || *metadata.ImageHeight != 554 {
		t.Fatalf("ExifTool stdin dimensions were not recognized: width=%v height=%v", metadata.ImageWidth, metadata.ImageHeight)
	}
	if metadata.BitDepth == nil || *metadata.BitDepth != 24 || metadata.ColorSpace == nil || *metadata.ColorSpace != "sRGB" || metadata.Compression == nil || *metadata.Compression != "JPEG" {
		t.Fatalf("PNG properties were not normalized: depth=%v color=%v compression=%v", metadata.BitDepth, metadata.ColorSpace, metadata.Compression)
	}
	var extra map[string]json.RawMessage
	if err := json.Unmarshal(metadata.Extra, &extra); err != nil {
		t.Fatalf("extra JSON invalid: %v", err)
	}
	if _, exists := extra["ExifTool:FileModifyDate"]; exists {
		t.Fatal("local file modification time must not be persisted")
	}
	if _, exists := extra["ExifTool:FileAccessDate"]; exists {
		t.Fatal("local file access time must not be persisted")
	}
	if _, exists := extra["ExifTool:FileInodeChangeDate"]; exists {
		t.Fatal("local inode change time must not be persisted")
	}
	if _, exists := extra["ExifTool:FilePermissions"]; exists {
		t.Fatal("local file permissions must not be persisted")
	}
	if string(extra["ExifTool:ImageSize"]) != `"356 554"` {
		t.Fatalf("unnamespaced image tag was not namespaced: %s", metadata.Extra)
	}
}

func TestMergeImageMetadataServerWinsAndBrowserFillsMissingValues(t *testing.T) {
	client := model.InspirationImageMetadata{Title: stringPointer("browser"), Author: stringPointer("client"), Extra: model.JSONB(`{"XMP:Shared":"browser","exifr:iptc:City":"Shanghai"}`)}
	server := model.InspirationImageMetadata{Title: stringPointer("server"), CameraModel: stringPointer("EOS"), Extra: model.JSONB(`{"XMP:Shared":"server"}`)}

	merged := MergeInspirationImageMetadata(client, server)
	if merged.Title == nil || *merged.Title != "server" {
		t.Fatalf("server should override the conflicting title: %v", merged.Title)
	}
	if merged.Author == nil || *merged.Author != "client" || merged.CameraModel == nil || *merged.CameraModel != "EOS" {
		t.Fatalf("missing values were not preserved/filled: %+v", merged)
	}
	var extra map[string]string
	if err := json.Unmarshal(merged.Extra, &extra); err != nil {
		t.Fatal(err)
	}
	if extra["XMP:Shared"] != "server" || extra["exifr:iptc:City"] != "Shanghai" {
		t.Fatalf("unexpected merged extra tags: %#v", extra)
	}
}

func TestCompressionPersistsOriginalMetadataAndPreservesItAfterFileUpdate(t *testing.T) {
	userID, inspirationID := uuid.New(), uuid.New()
	imageKey := localKey(uuid.New())
	sourceKey := localKey(uuid.New())
	content := tinyPNG(t)
	files := &metadataTestFiles{contents: map[string][]byte{imageKey: content, sourceKey: content}}
	repository := &metadataTestRepository{record: model.Inspiration{
		BaseModel: model.BaseModel{BusinessID: inspirationID}, UserID: userID, ImageKey: imageKey,
		AsyncVersion: 7, ImageMetadata: model.InspirationImageMetadata{Title: stringPointer("browser title")},
		Data: model.JSONB(`{"source_image_key":"` + sourceKey + `"}`),
	}}
	service := &InspirationService{inspirations: repository, files: files, maxImageBytes: 1 << 20}
	service.readImageMetadata = func(_ context.Context, original []byte) (model.InspirationImageMetadata, error) {
		if len(original) == 0 {
			t.Fatal("metadata reader did not receive original image bytes")
		}
		// 模拟解析期间用户用 metadata-only PATCH 更新记录，后台缺失字段写入不得覆盖该值。
		repository.record.ImageMetadata.Author = stringPointer("concurrent patch")
		return model.InspirationImageMetadata{Title: stringPointer("server title"), CameraModel: stringPointer("EOS R6")}, nil
	}

	err := service.processCompressionJob(inspirationCompressionJob{userID: userID, inspirationID: inspirationID,
		imageKey: imageKey, asyncVersion: 7})
	if err != nil {
		t.Fatalf("processCompressionJob() error = %v", err)
	}
	if repository.record.ImageMetadata.Title == nil || *repository.record.ImageMetadata.Title != "server title" {
		t.Fatalf("server metadata was not persisted: %+v", repository.record.ImageMetadata)
	}
	if repository.record.ImageMetadata.CameraModel == nil || *repository.record.ImageMetadata.CameraModel != "EOS R6" {
		t.Fatalf("camera metadata missing after compression metadata write: %+v", repository.record.ImageMetadata)
	}
	if repository.record.ImageMetadata.Author == nil || *repository.record.ImageMetadata.Author != "concurrent patch" {
		t.Fatalf("metadata-only update during parsing was overwritten: %+v", repository.record.ImageMetadata)
	}
	if repository.record.ImageMetadata.SourceFileSize == nil || *repository.record.ImageMetadata.SourceFileSize != int64(len(content)) {
		t.Fatalf("server original file size was not saved: %v", repository.record.ImageMetadata.SourceFileSize)
	}
	if repository.fileMetadataWrites != 1 {
		t.Fatalf("compression file metadata update count = %d, want 1", repository.fileMetadataWrites)
	}
}

func TestMetadataParseFailureDoesNotBlockCompressionAndStaleGenerationDoesNotWrite(t *testing.T) {
	userID, inspirationID := uuid.New(), uuid.New()
	imageKey := localKey(uuid.New())
	content := tinyPNG(t)
	files := &metadataTestFiles{contents: map[string][]byte{imageKey: content}}
	repository := &metadataTestRepository{record: model.Inspiration{
		BaseModel: model.BaseModel{BusinessID: inspirationID}, UserID: userID, ImageKey: imageKey,
		AsyncVersion: 3, ImageMetadata: model.InspirationImageMetadata{Title: stringPointer("browser fallback")},
		Data: model.JSONB(`{"schema_version":1}`),
	}}
	service := &InspirationService{inspirations: repository, files: files, maxImageBytes: 1 << 20,
		readImageMetadata: func(context.Context, []byte) (model.InspirationImageMetadata, error) {
			return model.InspirationImageMetadata{}, io.ErrUnexpectedEOF
		}}
	if err := service.processCompressionJob(inspirationCompressionJob{userID: userID, inspirationID: inspirationID,
		imageKey: imageKey, asyncVersion: 3}); err != nil {
		t.Fatalf("metadata parse failure blocked compression: %v", err)
	}
	if repository.record.ImageMetadata.Title == nil || *repository.record.ImageMetadata.Title != "browser fallback" || repository.fileMetadataWrites != 1 {
		t.Fatalf("client metadata or compression result was lost: %+v", repository.record)
	}
	if repository.record.ImageMetadata.SourceFileSize == nil || *repository.record.ImageMetadata.SourceFileSize != int64(len(content)) {
		t.Fatalf("original size should still be stored when ExifTool parsing fails: %v", repository.record.ImageMetadata.SourceFileSize)
	}

	repository.record.AsyncVersion = 4
	service.readImageMetadata = func(context.Context, []byte) (model.InspirationImageMetadata, error) {
		t.Fatal("stale generation must not invoke metadata extraction")
		return model.InspirationImageMetadata{}, nil
	}
	if err := service.processCompressionJob(inspirationCompressionJob{userID: userID, inspirationID: inspirationID,
		imageKey: imageKey, asyncVersion: 3}); err != nil {
		t.Fatalf("stale compression generation should be discarded: %v", err)
	}
	if repository.fileMetadataWrites != 1 {
		t.Fatalf("stale generation wrote file metadata: %d writes", repository.fileMetadataWrites)
	}
}

type metadataTestRepository struct {
	record             model.Inspiration
	fileMetadataWrites int
}

func (repository *metadataTestRepository) ValidateTagOwnership(context.Context, uuid.UUID, []uuid.UUID) error {
	return nil
}
func (repository *metadataTestRepository) CreateInspiration(context.Context, *model.Inspiration) error {
	return nil
}
func (repository *metadataTestRepository) ListInspirations(context.Context, uuid.UUID, []uuid.UUID, string, string, string, int, int) ([]model.Inspiration, int64, error) {
	return nil, 0, nil
}
func (repository *metadataTestRepository) FindInspirationByBusinessID(_ context.Context, userID, inspirationID uuid.UUID) (model.Inspiration, error) {
	if repository.record.UserID != userID || repository.record.BusinessID != inspirationID {
		return model.Inspiration{}, errors.New("record not found")
	}
	return repository.record, nil
}
func (repository *metadataTestRepository) UpdateInspiration(context.Context, uuid.UUID, uuid.UUID, map[string]any) (model.Inspiration, error) {
	return repository.record, nil
}
func (repository *metadataTestRepository) UpdateInspirationWithAsyncVersion(context.Context, uuid.UUID, uuid.UUID, map[string]any) (model.Inspiration, error) {
	return repository.record, nil
}
func (repository *metadataTestRepository) UpdateInspirationFileMetadataIfVersion(_ context.Context, userID, inspirationID uuid.UUID, imageKey string, version int64, hash *string, size *int64) (bool, error) {
	if repository.record.UserID != userID || repository.record.BusinessID != inspirationID || repository.record.ImageKey != imageKey || repository.record.AsyncVersion != version {
		return false, nil
	}
	repository.record.FileHash = hash
	repository.record.FileSize = size
	repository.fileMetadataWrites++
	return true, nil
}
func (repository *metadataTestRepository) UpdateInspirationImageMetadataIfVersion(_ context.Context, userID, inspirationID uuid.UUID, imageKey string, version int64, metadata model.InspirationImageMetadata) (bool, error) {
	if repository.record.UserID != userID || repository.record.BusinessID != inspirationID || repository.record.ImageKey != imageKey || repository.record.AsyncVersion != version {
		return false, nil
	}
	repository.record.ImageMetadata = MergeInspirationImageMetadata(repository.record.ImageMetadata, metadata)
	return true, nil
}
func (repository *metadataTestRepository) RemoveInspirationSourceImageKeyIfVersion(_ context.Context, userID, inspirationID uuid.UUID, imageKey string, version int64, sourceKey string) (bool, error) {
	if repository.record.UserID != userID || repository.record.BusinessID != inspirationID || repository.record.ImageKey != imageKey || repository.record.AsyncVersion != version {
		return false, nil
	}
	var data map[string]json.RawMessage
	if json.Unmarshal(repository.record.Data, &data) != nil || data == nil {
		return false, nil
	}
	var currentSourceKey string
	if json.Unmarshal(data["source_image_key"], &currentSourceKey) != nil || currentSourceKey != sourceKey {
		return false, nil
	}
	delete(data, "source_image_key")
	encoded, err := json.Marshal(data)
	if err != nil {
		return false, err
	}
	repository.record.Data = model.JSONB(encoded)
	return true, nil
}
func (repository *metadataTestRepository) IsInspirationFileReferenced(_ context.Context, imageKey string, exceptInspirationID uuid.UUID) (bool, error) {
	if repository.record.BusinessID != exceptInspirationID && (repository.record.ImageKey == imageKey || sourceImageKeyFromData(repository.record.Data) == imageKey) {
		return true, nil
	}
	return false, nil
}
func (repository *metadataTestRepository) DeleteInspiration(context.Context, uuid.UUID, uuid.UUID) error {
	return nil
}

type metadataTestFiles struct{ contents map[string][]byte }

func (files *metadataTestFiles) Open(key string) (io.ReadCloser, int64, string, error) {
	content, exists := files.contents[key]
	if !exists {
		return nil, 0, "", io.ErrUnexpectedEOF
	}
	return io.NopCloser(bytes.NewReader(content)), int64(len(content)), "image/png", nil
}
func (files *metadataTestFiles) Replace(_ context.Context, key string, content []byte) error {
	files.contents[key] = append([]byte(nil), content...)
	return nil
}
func (files *metadataTestFiles) Delete(_ context.Context, key string) error {
	delete(files.contents, key)
	return nil
}

func tinyPNG(t *testing.T) []byte {
	t.Helper()
	var buffer bytes.Buffer
	img := image.NewRGBA(image.Rect(0, 0, 2, 2))
	img.Set(0, 0, color.White)
	if err := png.Encode(&buffer, img); err != nil {
		t.Fatal(err)
	}
	return buffer.Bytes()
}

func localKey(id uuid.UUID) string       { return "local_20261016_" + id.String() + ".png" }
func stringPointer(value string) *string { return &value }
