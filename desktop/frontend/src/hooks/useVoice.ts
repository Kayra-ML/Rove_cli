import { useCallback, useEffect, useRef, useState } from "react";

export interface UseVoiceReturn {
  listening: boolean;
  supported: boolean;
  start: () => void;
  stop: () => void;
}

export function useVoice(onTranscript: (text: string) => void): UseVoiceReturn {
  const [listening, setListening] = useState(false);
  const recogRef = useRef<SpeechRecognition | null>(null);
  const supported =
    typeof window !== "undefined" &&
    !!(window.SpeechRecognition ?? window.webkitSpeechRecognition);

  const stop = useCallback(() => {
    recogRef.current?.stop();
  }, []);

  const start = useCallback(() => {
    if (!supported) return;
    const Ctor = window.SpeechRecognition ?? window.webkitSpeechRecognition;
    if (!Ctor) return;
    const recog = new Ctor();
    recog.continuous = false;
    recog.interimResults = true;
    recog.lang = navigator.language || "en-US";

    recog.onstart = () => setListening(true);
    recog.onend = () => setListening(false);
    recog.onerror = () => setListening(false);

    recog.onresult = (event: SpeechRecognitionEvent) => {
      let final = "";
      for (let i = event.resultIndex; i < event.results.length; i++) {
        const res = event.results[i];
        if (res.isFinal) {
          final += res[0].transcript;
        }
      }
      if (final) {
        onTranscript(final.trim());
      }
    };

    recogRef.current = recog;
    recog.start();
  }, [supported, onTranscript]);

  useEffect(() => {
    return () => {
      recogRef.current?.stop();
    };
  }, []);

  return { listening, supported, start, stop };
}