package shared

import (
	"bytes"
	"crypto/md5"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"net/url"
	"path/filepath"
	"strings"
	"time"
)

func CalculateHash(str string) string {
	hash := md5.Sum([]byte(str))
	return hex.EncodeToString(hash[:])
}

// TODO: return an ok bool, and improve the perf of filename
func GetCacheFilePath(imageURL string) string {
	hash := md5.Sum([]byte(imageURL))
	ext := filepath.Ext(imageURL)
	if ext == "" {
		ext = ".jpg"
	}

	filename := hex.EncodeToString(hash[:]) + ext
	return filepath.Join(TmpfsCacheDir, filename)
}

func ContentTypeForFormat(format string) (string, error) {
	log.Printf("🔥 ContentTypeForFormat: Looking for format='%s'", format)

	// Try format as-is first
	if contentType, ok := AllowedImgExtMap[format]; ok {
		log.Printf("🔥 ContentTypeForFormat: Found in AllowedImgExtMap: %s", contentType)
		return contentType, nil
	}
	if contentType, ok := VideoExtMap[format]; ok {
		log.Printf("🔥 ContentTypeForFormat: Found in VideoExtMap: %s", contentType)
		return contentType, nil
	}

	// Try with dot prefix (e.g., "sprites" -> ".sprites")
	formatWithDot := "." + format
	log.Printf("🔥 ContentTypeForFormat: Trying with dot: '%s'", formatWithDot)
	if contentType, ok := AllowedImgExtMap[formatWithDot]; ok {
		log.Printf("🔥 ContentTypeForFormat: Found in AllowedImgExtMap with dot: %s", contentType)
		return contentType, nil
	}
	if contentType, ok := VideoExtMap[formatWithDot]; ok {
		log.Printf("🔥 ContentTypeForFormat: Found in VideoExtMap with dot: %s", contentType)
		return contentType, nil
	}

	log.Printf("🔥 ContentTypeForFormat: NOT FOUND! format='%s', formatWithDot='%s'", format, formatWithDot)
	return "", fmt.Errorf("unsupported format")
}

// Benchmark function for logging execution time
func Bench(msg string) func() {
	start := time.Now()
	return func() {
		log.Printf("%s took %v", msg, time.Since(start))
	}
}

const eachLinkHeaderSep = "; "

func BuildLinkHeader(assetURL *url.URL) string {
	path := assetURL.Path
	query := strings.TrimSpace(assetURL.Query().Encode())

	if path == "" && query == "" {
		return ""
	}

	// byt := &bytes.Buffer{}
	// byt.WriteString("<")
	// byt.WriteString(path)
	//
	// if query != "" {
	// 	byt.WriteString("?")
	// 	byt.WriteString(query)
	// }
	// byt.WriteString(">")
	// byt.WriteString(eachLinkHeaderSep)
	// byt.WriteString("rel=preload")
	// byt.WriteString(eachLinkHeaderSep)
	// byt.WriteString("as=image")
	//
	// return byt.String()
	// return fmt.Sprintf("<%s?%s>; rel=preload; as=image", path, query)
	return fmt.Sprintf("<%s>; rel=preload; as=image", assetURL.String())
}

func BuildAllLinkHeader(assetURL []*url.URL) string {
	linkHeaders := make([]string, len(assetURL))

	for i, u := range assetURL {
		linkHeaders[i] = BuildLinkHeader(u)
	}

	return strings.Join(linkHeaders, ", ")
}

func BuildLinkHeaderFromStr(assetURLStr string, pathPrefix string) string {
	pathPrefix = MustBeginStr(pathPrefix, "/") // make sure it starts with /

	idx := strings.Index(assetURLStr, pathPrefix)
	if idx == -1 {
		return ""
	}

	// pathWithQuery := assetURLStr[idx:]
	return fmt.Sprintf("<%s>; rel=preload; as=image", assetURLStr)
}

func SanitizeSizes(sizesParam []string) [][2]int {
	sizes := [][2]int{}

	for _, size := range sizesParam {
		var width, height int
		fmt.Sscanf(size, "%dx%d", &width, &height)

		if width > 0 || height > 0 {
			sizes = append(sizes, [2]int{width, height})
		} else if width == 0 && height == 0 {
			sizes = append([][2]int{{0, 0}}, sizes...) // prepend for 0x0
		}
	}

	if len(sizesParam) == 0 {
		sizes = append(sizes, [2]int{0, 0})
	}

	if len(sizes) > 5 {
		// Limit to the first 5 sizes
		sizes = sizes[0:5]
	}

	return sizes
}

func ToSizeStr(dims ...int) (string, bool) {
	// width, height
	var ok bool

	if len(dims) == 0 {
		return "0x0", ok
	}

	if len(dims) < 2 {
		dims = append(dims, 0)
	}

	byts := &bytes.Buffer{}
	if dims[0] > 0 {
		byts.WriteString(fmt.Sprintf("%d", dims[0]))
		ok = true
	}
	byts.WriteString("x")
	if dims[1] > 0 {
		byts.WriteString(fmt.Sprintf("%d", dims[1]))
		ok = true
	}

	return byts.String(), ok
}

// Converts widthxheight params to size key
// as defined by IdSizeMap, destructively
// For example: 600w is mapped to 600x by post process worker
// this decouples the client from having to know about existing
// post processed images.
func TransformSizesForApi(sizeParams []string) {
	for i, size := range sizeParams {
		if v, ok := IdSizeMap[size]; ok {
			sizeParams[i] = v
		}
	}
}

// Converts image size keys to widthxheight params
// as defined by IdSizeMapRev, destructively
// For example: 600x is mapped to 600w by post process worker
// this decouples the client from having to know about existing
// post processed images.
func TransformSizesForWorker(sizeParams []string) {
	for i, size := range sizeParams {
		if v, ok := IdSizeMapRev[size]; ok {
			sizeParams[i] = v
		}
	}
}

func FlushResponse(w http.ResponseWriter, writer func(w http.ResponseWriter) bool) error {
	log.Println("flushing headers prematurely")

	// w.WriteHeader(http.StatusContinue)
	ok := writer(w)
	if !ok {
		return nil
	}

	flusher, ok := w.(http.Flusher)
	if !ok {
		return fmt.Errorf("streaming not supported")
	}

	flusher.Flush()
	return nil
}

func IsOfMediaType(pathOrExt string, allowedExts map[string]string) bool {
	ext := strings.ToLower(filepath.Ext(pathOrExt))
	if ext == "" {
		return true
	}

	_, ok := allowedExts[ext]
	return ok
}

func Dumps(v any) string {
	b, err := json.MarshalIndent(v, " ", " ")
	if err != nil {
		return fmt.Sprintf("%+v", v)
	}

	return string(b)
}

// ExplodeFileName returns the filename and the ext
func ExplodeFileName(filePath string) (string, string, bool) {
	base := filepath.Base(filePath)
	ext := filepath.Ext(filePath)

	fileName := strings.TrimSpace(strings.TrimSuffix(base, ext))

	return fileName, ext, ext != "." || fileName != ""
}

func AppendSizeToFileName(filePath string, sizeID string, args ...string) string {
	fileName, ext, ok := ExplodeFileName(filePath)
	if !ok {
		return filePath
	}

	if len(args) > 0 && strings.HasPrefix(args[0], ".") {
		ext = args[0]
	}

	if ok {
		return fmt.Sprintf("%s_%s%s", fileName, sizeID, ext)
	}

	return filePath
}

func ExtractSizeIDFromFile(fileName string) string {
	ext := filepath.Ext(fileName)
	if ext != "." {
		fileName = strings.TrimSuffix(fileName, ext)
	}

	splits := strings.SplitN(fileName, "@", 2)

	if len(splits) < 2 {
		return ""
	}

	return fmt.Sprintf("@%s", splits[1])
}

// Exctracts the Bucket and S3Key from url
//
//	Input: https://redernet-image-data.s3.amazonaws.com/stg/user_generated/images/fo/ld/er/img_hhh.png
//
// Returns
// bucket: redernet-image-data
// key: stg/user_generated/images/fo/ld/er/img_hhh.png
func ExtractBucketAndKeyFromS3(urlStr string) (bucket string, key string, ok bool) {
	u, err := url.Parse(urlStr)
	if err != nil {
		return
	}

	hostSplits := strings.Split(u.Host, ".s3.amazonaws.com")
	if len(hostSplits) < 2 {
		return
	}

	bucket = hostSplits[0]
	key = strings.TrimPrefix(u.Path, "/")

	ok = true

	return
}

var optimizedMediaPrefix = []string{"optimized"}

func GetResizedS3Key(paths ...string) string {
	paths = append(optimizedMediaPrefix, paths...)
	return filepath.Join(paths...)
}

func ReplaceWithResizedURL(path string, sizeID string) string {
	_, ok := IdSizeMap[sizeID]
	if !ok { // this shouldn't happen
		return ""
	}

	log.Println("sizeID", sizeID)

	dir := filepath.Dir(path)
	origFileName, _, ok := ExplodeFileName(path)
	if !ok {
		return ""
	}

	newFileName := fmt.Sprintf("%s_%s.webp", origFileName, sizeID)

	log.Println("replacing url with resized url by convention", origFileName, newFileName)

	return GetResizedS3Key(dir, newFileName)
}
