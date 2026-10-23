package genre

import (
	"os"
	"path/filepath"
	"strings"

	"github.com/go-flac/flacvorbis"
	flac "github.com/go-flac/go-flac"
)

// WriteFLAC sets the GENRE Vorbis comment of the FLAC at path, keeping every other comment.
// It writes a temp file next to path and renames it over, so a failure leaves path untouched.
func WriteFLAC(path, genre string) error {
	f, err := flac.ParseFile(path)
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
	kept := vc.Comments[:0]
	for _, c := range vc.Comments {
		if k, _, _ := strings.Cut(c, "="); !strings.EqualFold(k, flacvorbis.FIELD_GENRE) {
			kept = append(kept, c)
		}
	}
	vc.Comments = kept
	if err := vc.Add(flacvorbis.FIELD_GENRE, genre); err != nil {
		return err
	}
	blk := vc.Marshal()
	if at >= 0 {
		f.Meta[at] = &blk
	} else { // right after STREAMINFO
		f.Meta = append(f.Meta[:1], append([]*flac.MetaDataBlock{&blk}, f.Meta[1:]...)...)
	}

	st, err := os.Stat(path)
	if err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), "genre-*.tmp")
	if err != nil {
		return err
	}
	_, err = tmp.Write(f.Marshal())
	if cerr := tmp.Close(); err == nil {
		err = cerr
	}
	if err == nil {
		err = os.Chmod(tmp.Name(), st.Mode().Perm())
	}
	if err == nil {
		err = os.Rename(tmp.Name(), path)
	}
	if err != nil {
		os.Remove(tmp.Name())
	}
	return err
}
