"""Windows stand-in for the `eunjeon` package, which g2pkk imports by name.

MeloTTS's Korean frontend (g2pkk) hardcodes a different morphological analyzer
per platform: `python-mecab-ko` on Unix, `eunjeon` on Windows. `eunjeon` has no
wheels and needs a full MSVC C++ toolchain to build from source. `mecab-ko`
ships prebuilt Windows wheels of the same underlying MeCab, but exposes only the
raw SWIG `Tagger`, not the `.pos()` method g2pkk calls.

This module bridges the two. It lives next to server.py so that running
`python sidecar/server.py` puts it on sys.path ahead of anything else, which is
what makes g2pkk's `find_spec("eunjeon")` succeed.

Without it g2pkk still works, but silently skips the POS-dependent rules —
the 의 particle (나의 -> 나에), ㄹ-ending tensification, and nasal-stem
tensification (안다 -> 안따). Those are common enough in beginner Korean that
losing them would teach the wrong pronunciation.
"""

import mecab_ko
import mecab_ko_dic


class Mecab:
    """The subset of eunjeon.Mecab that g2pkk actually uses: pos()."""

    def __init__(self, dicpath: str | None = None) -> None:
        dicdir = dicpath or mecab_ko_dic.DICDIR
        # MeCab parses this argument string with shell-like splitting, which
        # eats Windows backslashes and turns the path into one long token.
        # Forward slashes survive; quotes handle any spaces in the path.
        dicdir = dicdir.replace("\\", "/")
        self._tagger = mecab_ko.Tagger(f'-d "{dicdir}"')

    def pos(self, text: str) -> list[tuple[str, str]]:
        """Return [(surface, tag), ...], the shape g2pkk's annotate() expects."""
        tagged = []
        for line in self._tagger.parse(text).splitlines():
            if not line or line == "EOS":
                continue
            surface, _, feature = line.partition("\t")
            if not feature:
                continue
            # The first feature field is the part-of-speech tag; g2pkk only
            # ever looks at that one.
            tagged.append((surface, feature.split(",")[0]))
        return tagged

    def morphs(self, text: str) -> list[str]:
        return [surface for surface, _ in self.pos(text)]

    def nouns(self, text: str) -> list[str]:
        return [surface for surface, tag in self.pos(text) if tag.startswith("N")]
