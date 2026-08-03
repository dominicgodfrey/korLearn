"""Korean text-to-speech sidecar for korLearn.

Speaks one protocol, POST /synth with {"text": ..., "voice": ...} returning a
wav body, so the Go server never learns which engine is behind it. Kokoro was
the original plan and turned out to have no Korean voices at all; MeloTTS
replaced it because its Korean frontend (g2pkk) applies real phonology —
받침 resolution and consonant assimilation — rather than transliterating.

Run it separately from the Go binary:

    sidecar/.venv/Scripts/python.exe sidecar/server.py --port 8123

The model loads on the first request rather than at startup, so a failure
shows up as a readable HTTP error instead of a dead process, and the Go side
can boot in any order.
"""

import argparse
import io
import json
import logging
import os
import sys
import tempfile
import threading
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer

# This directory holds eunjeon.py, the Windows shim g2pkk imports by name.
# Inserted explicitly rather than relying on sys.path[0] so the server still
# works under PYTHONSAFEPATH or when imported rather than run.
sys.path.insert(0, os.path.dirname(os.path.abspath(__file__)))

# NLTK refuses to import any module whose file lives under the current working
# directory. The venv sits inside the repo at sidecar/.venv, so running this
# from the repo root makes every one of NLTK's own dependencies look like a
# working-directory import and synthesis fails with a confusing security error.
# Moving off the repo before importing anything that pulls in NLTK sidesteps it
# for good; nothing here resolves paths relatively.
os.chdir(tempfile.gettempdir())

import soundfile as sf

log = logging.getLogger("tts")

# Bounds the work a single request can ask for. The Go side caps text length
# too; this is the backstop for anything else that talks to the sidecar.
MAX_TEXT_LEN = 500


class Synthesizer:
    """Loads MeloTTS once and serializes access to it.

    Inference is not thread-safe and the model is the only copy in memory, so
    concurrent requests are queued rather than run in parallel. Synthesis is
    fast and every result is cached by the caller, so the queue stays short.
    """

    def __init__(self, language: str, device: str) -> None:
        self.language = language
        self.device = device
        self._lock = threading.Lock()
        self._model = None
        self._speakers: dict[str, int] = {}

    def _ensure_loaded(self) -> None:
        if self._model is not None:
            return
        # Imported lazily: it pulls in torch, which is slow enough to make
        # startup look hung.
        from melo.api import TTS

        log.info("loading MeloTTS language=%s device=%s", self.language, self.device)
        self._model = TTS(language=self.language, device=self.device)
        self._speakers = dict(self._model.hps.data.spk2id)
        log.info("loaded; speakers=%s", list(self._speakers))

    def synthesize(self, text: str, voice: str = "") -> bytes:
        with self._lock:
            self._ensure_loaded()

            if voice and voice in self._speakers:
                speaker_id = self._speakers[voice]
            elif voice:
                raise KeyError(f"unknown voice {voice!r}; have {list(self._speakers)}")
            else:
                speaker_id = next(iter(self._speakers.values()))

            audio = self._model.tts_to_file(text, speaker_id, None, speed=1.0)

        buf = io.BytesIO()
        # PCM_16 so browsers can play it back without conversion.
        sf.write(buf, audio, self._model.hps.data.sampling_rate, format="WAV", subtype="PCM_16")
        return buf.getvalue()


class Handler(BaseHTTPRequestHandler):
    synth: Synthesizer  # set on the class by main()

    def do_GET(self) -> None:
        if self.path.split("?")[0] != "/health":
            self._send_json(404, {"error": "not found"})
            return
        self._send_json(200, {"status": "ok", "loaded": self.synth._model is not None})

    def do_POST(self) -> None:
        if self.path.split("?")[0] != "/synth":
            self._send_json(404, {"error": "not found"})
            return

        try:
            length = int(self.headers.get("Content-Length") or 0)
            payload = json.loads(self.rfile.read(length) or b"{}")
        except (ValueError, json.JSONDecodeError) as exc:
            self._send_json(400, {"error": f"invalid request body: {exc}"})
            return

        text = (payload.get("text") or "").strip()
        if not text:
            self._send_json(400, {"error": "text is required"})
            return
        if len(text) > MAX_TEXT_LEN:
            self._send_json(400, {"error": f"text exceeds {MAX_TEXT_LEN} characters"})
            return

        try:
            audio = self.synth.synthesize(text, payload.get("voice") or "")
        except KeyError as exc:
            self._send_json(400, {"error": str(exc)})
            return
        except Exception as exc:  # model errors must not kill the server
            log.exception("synthesis failed")
            self._send_json(500, {"error": f"synthesis failed: {exc}"})
            return

        self.send_response(200)
        self.send_header("Content-Type", "audio/wav")
        self.send_header("Content-Length", str(len(audio)))
        self.end_headers()
        self.wfile.write(audio)

    def _send_json(self, status: int, body: dict) -> None:
        encoded = json.dumps(body).encode("utf-8")
        self.send_response(status)
        self.send_header("Content-Type", "application/json")
        self.send_header("Content-Length", str(len(encoded)))
        self.end_headers()
        self.wfile.write(encoded)

    def log_message(self, fmt: str, *args) -> None:
        log.info("%s - %s", self.address_string(), fmt % args)


def main() -> None:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--host", default="localhost")
    parser.add_argument("--port", type=int, default=8123)
    parser.add_argument("--language", default="KR")
    parser.add_argument("--device", default="auto", help="auto, cpu, or cuda")
    args = parser.parse_args()

    logging.basicConfig(level=logging.INFO, format="%(asctime)s %(levelname)s %(message)s")

    Handler.synth = Synthesizer(args.language, args.device)
    server = ThreadingHTTPServer((args.host, args.port), Handler)
    log.info("tts sidecar listening on http://%s:%d", args.host, args.port)
    try:
        server.serve_forever()
    except KeyboardInterrupt:
        log.info("shutting down")


if __name__ == "__main__":
    main()
