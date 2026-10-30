package tags

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/go-flac/flacpicture"
	"github.com/go-flac/flacvorbis"
	flac "github.com/go-flac/go-flac"
)

// Vorbis comment names that differ from the upper-cased tag name.
var vorbisName = map[string]string{"album_artist": "ALBUMARTIST", "track": "TRACKNUMBER", "disc": "DISCNUMBER"}

func toVorbis(k string) string {
	if n, ok := vorbisName[k]; ok {
		return n
	}
	return strings.ToUpper(k)
}

func fromVorbis(n string) string {
	for k, v := range vorbisName {
		if strings.EqualFold(n, v) {
			return k
		}
	}
	return strings.ToLower(n)
}

func readFLAC(path string) (Tags, bool, error) {
	f, err := parseFLAC(path)
	if err != nil {
		return nil, false, err
	}
	t, hasCover := Tags{}, false
	for _, m := range f.Meta {
		switch m.Type {
		case flac.Picture:
			hasCover = true
		case flac.VorbisComment:
			vc, err := flacvorbis.ParseFromMetaDataBlock(*m)
			if err != nil {
				return nil, false, err
			}
			for _, c := range vc.Comments {
				if k, v, ok := strings.Cut(c, "="); ok {
					if k = fromVorbis(k); t[k] == "" { // the first of repeated comments wins
						t[k] = v
					}
				}
			}
		}
	}
	return t, hasCover, nil
}

func writeFLAC(path string, t Tags, cover []byte) error {
	f, err := parseFLAC(path)
	if err != nil {
		return err
	}
	vc, at := flacvorbis.New(), -1
	for i, m := range f.Meta {
		if m.Type == flac.VorbisComment {
			if vc, err = flacvorbis.ParseFromMetaDataBlock(*m); err != nil {
				return err
			}
			at = i
			break
		}
	}
	replaced := map[string]bool{}
	for k := range t {
		replaced[toVorbis(k)] = true
	}
	kept := vc.Comments[:0]
	for _, c := range vc.Comments {
		if k, _, _ := strings.Cut(c, "="); !replaced[strings.ToUpper(k)] {
			kept = append(kept, c)
		}
	}
	vc.Comments = kept
	for k, v := range t {
		if v == "" { // an empty value only removes
			continue
		}
		if err := vc.Add(toVorbis(k), v); err != nil {
			return err
		}
	}
	blk := vc.Marshal()
	if at >= 0 {
		f.Meta[at] = &blk
	} else { // right after STREAMINFO
		f.Meta = append(f.Meta[:1], append([]*flac.MetaDataBlock{&blk}, f.Meta[1:]...)...)
	}

	if cover != nil {
		mime := map[string]string{".jpg": "image/jpeg", ".png": "image/png"}[imageExt(cover)]
		if mime == "" {
			return fmt.Errorf("cover: only JPEG and PNG are supported")
		}
		pic, err := flacpicture.NewFromImageData(flacpicture.PictureTypeFrontCover, "", cover, mime)
		if err != nil {
			return fmt.Errorf("cover: %w", err)
		}
		pb := pic.Marshal()
		meta := f.Meta[:0]
		for _, m := range f.Meta { // drop the old front cover, keep other pictures
			if old, err := flacpicture.ParseFromMetaDataBlock(*m); err == nil && old.PictureType == flacpicture.PictureTypeFrontCover {
				continue
			}
			meta = append(meta, m)
		}
		f.Meta = append(meta, &pb)
	}

	tmp, err := os.CreateTemp(filepath.Dir(path), ".tags-*.tmp")
	if err != nil {
		return err
	}
	_, err = tmp.Write(f.Marshal())
	if cerr := tmp.Close(); err == nil {
		err = cerr
	}
	if err != nil {
		os.Remove(tmp.Name())
		return err
	}
	return replaceFile(tmp.Name(), path)
}

// parseFLAC turns go-flac's panic on truncated input (metadata but no frames) into an error.
func parseFLAC(path string) (f *flac.File, err error) {
	defer func() {
		if r := recover(); r != nil {
			f, err = nil, fmt.Errorf("not a valid FLAC file: %v", r)
		}
	}()
	return flac.ParseFile(path)
}
