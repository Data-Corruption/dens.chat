// Link previews, as a browser new to Dens is asked about them: on
// the first run's local password, or, where that's set, on a screen of
// their own once the browser is paired. One switch, on, covers YouTube's
// players and Steam's cards; turned off, it opens into a switch for each.

import { Toggle } from '../ui/components.jsx';

export const ALL_PREVIEWS = { all: true, youtube: true, steam: true };
const NO_PREVIEWS = { all: false, youtube: false, steam: false };

/** previewsKept is what a choice keeps: both while Link previews is on,
    or each as its own switch says */
export function previewsKept(choice) {
    return choice.all ? { youtube: true, steam: true } : { youtube: choice.youtube, steam: choice.steam };
}

export function LinkPreviews({ choice, onChange }) {
    return (
        <div class="flex flex-col gap-3" data-link-previews>
            <Toggle id="link-previews" label="Link previews" hint="YouTube and Steam see your IP address when previews load." checked={choice.all}
                onChange={(on) => onChange(on ? ALL_PREVIEWS : NO_PREVIEWS)} />
            {!choice.all && (
                <>
                    <Toggle id="link-previews-youtube" label="Play YouTube videos in Dens" checked={choice.youtube}
                        onChange={(on) => onChange({ ...choice, youtube: on })} />
                    <Toggle id="link-previews-steam" label="Show Steam games as cards" checked={choice.steam}
                        onChange={(on) => onChange({ ...choice, steam: on })} />
                </>
            )}
        </div>
    );
}
