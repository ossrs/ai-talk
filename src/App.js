import React from 'react';
import './App.css';

function useIsMobile() {
  const [isMobile, setIsMobile] = React.useState(false);

  function handleWindowSizeChange() {
    setIsMobile(window.innerWidth <= 768);
  }
  React.useEffect(() => {
    window.addEventListener('resize', handleWindowSizeChange);
    return () => {
      window.removeEventListener('resize', handleWindowSizeChange);
    }
  }, [setIsMobile]);

  return isMobile;
}

function App() {
  const [btnClassName, setBtnClassName] = React.useState('StaticButton');
  const [loading, setLoading] = React.useState(false);
  const mediaRecorderRef = React.useRef(null);
  const audioChunkRef = React.useRef([]);
  const [started, setStarted] = React.useState(false);
  const audioPlayerRef = React.useRef(null);
  const isMobile = useIsMobile();

  const [showShortLogs, setShowShortLogs] = React.useState(true);
  const [longLogRenders, setLongLogRenders] = React.useState([]);
  const longLogs = React.useRef([]);
  const [shortLogRenders, setShortLogRenders] = React.useState([]);
  const shortLogs = React.useRef([]);

  const writeShortLog = React.useCallback((msg) => {
    const date = new Date();
    const hours = date.getHours().toString().padStart(2, '0');
    const minutes = date.getMinutes().toString().padStart(2, '0');
    const seconds = date.getSeconds().toString().padStart(2, '0');

    const log = `[${hours}:${minutes}:${seconds}]: ${msg}`;
    console.log(log);

    shortLogs.current = [log, ...shortLogs.current];
    setShortLogRenders(shortLogs.current);
  }, [shortLogs, setShortLogRenders]);

  const writeLongLog = React.useCallback((msg) => {
    const date = new Date();
    const hours = date.getHours().toString().padStart(2, '0');
    const minutes = date.getMinutes().toString().padStart(2, '0');
    const seconds = date.getSeconds().toString().padStart(2, '0');

    const log = `[${hours}:${minutes}:${seconds}]: ${msg}`;
    console.log(log);

    longLogs.current = [log, ...longLogs.current];
    setLongLogRenders(longLogs.current);
  }, [longLogs, setLongLogRenders]);

  React.useEffect(() => {
    writeLongLog('App started');
  }, [writeLongLog]);

  const startRecording = React.useCallback(() => {
    if (mediaRecorderRef.current) return;

    setBtnClassName('DynamicButton');
    writeLongLog("===========================");
    writeShortLog("=============");

    navigator.mediaDevices.getUserMedia(
      { audio: true }
    ).then((stream) => {
      // See https://www.sitelint.com/lab/media-recorder-supported-mime-type/
      mediaRecorderRef.current = new MediaRecorder(stream);
      mediaRecorderRef.current.addEventListener("dataavailable", ({ data }) => {
        audioChunkRef.current.push(data);
        writeLongLog(`Event: dataavailable ${data.size} bytes`);
      });
      mediaRecorderRef.current.start();
      writeLongLog(`Event: Recording started`);
    }).catch(error => alert(error));
  }, [writeLongLog, writeShortLog, setBtnClassName, mediaRecorderRef, audioChunkRef]);

  const stopRecording = React.useCallback(() => {
    if (!mediaRecorderRef.current) return;

    mediaRecorderRef.current.addEventListener("stop", async () => {
      writeLongLog(`Event: stopping, ${audioChunkRef.current.length} chunks`);

      setLoading(true);

      try {
        // Upload the user input audio to the server.
        const uuid = await new Promise((resolve, reject) => {
          writeLongLog(`ASR: Uploading ${audioChunkRef.current.length} chunks`);
          const audioBlob = new Blob(audioChunkRef.current);
          audioChunkRef.current = [];

          // It can be aac or ogg codec.
          const formData = new FormData();
          formData.append('file', audioBlob, 'input.audio');

          fetch('/api/ai-talk/upload/', {
            method: 'POST',
            body: formData,
          }).then(response => {
            return response.json();
          }).then((data) => {
            writeLongLog(`ASR: Upload success: ${data.data.uuid} ${data.data.asr}`);
            writeShortLog(`You: ${data.data.asr}`);
            resolve(data.data.uuid);
          }).catch((error) => reject(error));
        });

        // Get the AI generated audio from the server.
        while (true) {
          writeLongLog(`TTS: Requesting ${uuid} response audios`);
          let readyUUID = null;
          while (!readyUUID) {
            const resp = await new Promise((resolve, reject) => {
              fetch(`/api/ai-talk/question/?rid=${uuid}`, {
                method: 'POST',
              }).then(response => {
                return response.json();
              }).then((data) => {
                if (data?.data?.uuid) {
                  writeLongLog(`TTS: Audio ready: ${data.data.uuid} ${data.data.tts}`);
                  writeShortLog(`Bot: ${data.data.tts}`);
                }
                resolve(data.data);
              }).catch(error => reject(error));
            });

            if (!resp.uuid) {
              break;
            }

            if (resp.processing) {
              await new Promise((resolve) => setTimeout(resolve, 300));
              continue;
            }

            readyUUID = resp.uuid;
          }

          // All audios are played.
          if (!readyUUID) {
            writeLongLog(`TTS: All audios are played.`);
            writeLongLog("===========================");
            break;
          }

          // Play the AI generated audio.
          await new Promise(resolve => {
            const url = `/api/ai-talk/tts/?rid=${uuid}&uuid=${readyUUID}`;
            writeLongLog(`TTS: Playing ${url}`);

            const listener = () => {
              audioPlayerRef.current.removeEventListener('ended', listener);
              writeLongLog(`TTS: Played ${url} done.`);
              resolve();
            };
            audioPlayerRef.current.addEventListener('ended', listener);

            audioPlayerRef.current.src = url;
            audioPlayerRef.current.play().catch(error => {
              writeLongLog(`TTS: Play ${url} failed: ${error}`);
              resolve();
            });
          });

          // Remove the AI generated audio.
          await new Promise((resolve, reject) => {
            fetch(`/api/ai-talk/remove/?rid=${uuid}&uuid=${readyUUID}`, {
              method: 'POST',
            }).then(response => {
              return response.json();
            }).then((data) => {
              writeLongLog(`TTS: Audio removed: ${readyUUID}`);
              resolve();
            }).catch(error => reject(error));
          });
        }
      } catch (e) {
        alert(e);
      } finally {
        setLoading(false);
        setBtnClassName('StaticButton');
      }
    });

    writeLongLog(`Event: stop, ${audioChunkRef.current.length} chunks`);
    mediaRecorderRef.current.stop();
    mediaRecorderRef.current = null;
  }, [writeLongLog, writeShortLog, setBtnClassName, mediaRecorderRef, audioChunkRef, setLoading, audioPlayerRef]);

  const onStart = React.useCallback(() => {
    writeLongLog("Start the app");
    writeShortLog("Start the app");

    const audio = new Audio("/api/ai-talk/examples/silent.aac");
    audio.loop = false;
    audio.play();
    audioPlayerRef.current = audio;
    setStarted(true);
  }, [writeLongLog, writeShortLog, setStarted, audioPlayerRef]);

  React.useEffect(() => {
    if (!started) return;

    const handleKeyDown = (e) => {
      if (e.key !== 'r' && e.key !== '\\') return;
      startRecording();
    };
    const handleKeyUp = (e) => {
      if (e.key !== 'r' && e.key !== '\\') return;
      setTimeout(() => {
        stopRecording();
      }, 800);
    };

    window.addEventListener('keydown', handleKeyDown);
    window.addEventListener('keyup', handleKeyUp);
    return () => {
      window.removeEventListener('keydown', handleKeyDown);
      window.removeEventListener('keyup', handleKeyUp);
    };
  }, [started]);

  return (<div className="App">
    <header className="App-header">
      {!started && <button className='StartButton' onClick={(e) => {
        onStart();
      }}>Click to start</button>}
      {started && <button
        onTouchStart={(e) => {
          startRecording();
        }}
        onTouchEnd={(e) => {
          setTimeout(() => {
            stopRecording();
          }, 800);
        }}
        className={btnClassName}
        disabled={loading}
      >{isMobile ? 'Press to talk' : 'Press the R key to talk'}</button>}
    </header>
    <p>
      <button onClick={(e) => {
        setShowShortLogs(!showShortLogs);
      }}>{showShortLogs ? 'Detail Logs' : 'Short Logs'}</button> &nbsp;
      <button onClick={(e) => {
        const audio = new Audio("/api/ai-talk/examples/example.aac");
        audio.loop = false;
        audio.play();
      }}>Example aac</button> &nbsp;
    </p>
    <ul className='LogPanel'>
      {!showShortLogs && longLogRenders.map((log, index) => {
        return (<li key={index}>{log}</li>);
      })}
      {showShortLogs && shortLogRenders.map((log, index) => {
        return (<li key={index}>{log}</li>);
      })}
    </ul>
  </div>);
}

export default App;
