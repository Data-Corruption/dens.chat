package denclient

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"image"
	"io"

	"github.com/Data-Corruption/dens.chat/internal/denproto"
	"github.com/Data-Corruption/dens.chat/internal/media"
)

// UploadAnimated makes an animated GIF or WebP into this member's picture
// or banner (M15.1), which picture names, and uploads it for their profile
// to use, as Upload does; replaces is the picture it takes the place of, if
// any. The page sends the file as it is, with the part of it the member
// framed, in its pixels, and this service makes each frame again, cropped
// and scaled, as a GIF of the picture's size, which a page's canvas can't.
func (m *Manager) UploadAnimated(ctx context.Context, denID, picture string, crop image.Rectangle, name string, size int64, body io.Reader,
	replaces string) (Uploaded, error) {
	c, err := m.find(denID)
	if err != nil {
		return Uploaded{}, err
	}
	if replaces != "" {
		if err := checkID("file", replaces); err != nil {
			return Uploaded{}, err
		}
	}
	p, err := animatedPicture(picture, crop)
	if err != nil {
		return Uploaded{}, inputError(err)
	}
	tooLarge := inputError(errors.New("This animation is too large to make a picture of: it has too many frames, or they're too large. Try a shorter or smaller one."))
	if size > media.MaxAnimationInput {
		return Uploaded{}, tooLarge
	}
	data, err := io.ReadAll(io.LimitReader(body, media.MaxAnimationInput+1))
	if err != nil {
		return Uploaded{}, err
	}
	if len(data) > media.MaxAnimationInput {
		return Uploaded{}, tooLarge
	}
	k := media.Sniff(data[:min(len(data), media.SniffLen)])
	if k != media.GIF && k != media.WebP {
		return Uploaded{}, inputError(errors.New("Only a GIF or a WebP can be an animated picture."))
	}
	select {
	case m.animating <- struct{}{}:
	case <-ctx.Done():
		return Uploaded{}, ctx.Err()
	}
	gif, err := media.CropAnimation(ctx, data, k, p)
	<-m.animating
	switch {
	case errors.Is(err, media.ErrAnimationTooLarge):
		return Uploaded{}, tooLarge
	case errors.Is(err, media.ErrAnimationTooLong):
		return Uploaded{}, inputError(fmt.Errorf("This animation comes out larger than the %d MB a %s can be. Try a shorter one.",
			p.MaxSize>>20, map[string]string{denproto.ProfileAvatar: "picture", denproto.ProfileBanner: "banner"}[picture]))
	case errors.Is(err, media.ErrNotFramed):
		return Uploaded{}, inputError(errors.New("The part of the picture you framed isn't on it."))
	case errors.Is(err, media.ErrMalformed):
		return Uploaded{}, inputError(errors.New("This file is damaged, or isn't the kind of image it starts out as, so it can't be a picture."))
	case err != nil:
		return Uploaded{}, err
	}
	return m.uploadTo(ctx, c, "", name, int64(len(gif)), bytes.NewReader(gif), replaces)
}

// animatedPicture is what an animated picture's GIF is to be: the part
// framed, which must have the picture's shape give or take the rounding of
// a crop, at its own size, but at most half a still's sides and at least a
// picture's least; and the most it may take.
func animatedPicture(picture string, crop image.Rectangle) (media.AnimatedPicture, error) {
	w, h := crop.Dx(), crop.Dy()
	switch picture {
	case denproto.ProfileAvatar:
		if w < 1 || w-h > 1 || h-w > 1 {
			return media.AnimatedPicture{}, errors.New("A picture is square.")
		}
		side := min(max(w, denproto.MinAvatarSide), denproto.MaxAnimatedAvatarSide)
		return media.AnimatedPicture{Crop: crop, Width: side, Height: side, MaxSize: denproto.MaxAnimatedAvatarSize}, nil
	case denproto.ProfileBanner:
		if h < 1 || w*100 < h*290 || w*100 > h*310 {
			return media.AnimatedPicture{}, errors.New("A banner is three times as wide as it's high.")
		}
		wide := min(max(w, denproto.MinBannerWide), denproto.MaxAnimatedBannerWide) / 3 * 3
		return media.AnimatedPicture{Crop: crop, Width: wide, Height: wide / 3, MaxSize: denproto.MaxAnimatedBannerSize}, nil
	}
	return media.AnimatedPicture{}, fmt.Errorf("A profile has no %q picture.", picture)
}
