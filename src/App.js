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

function useIsOssrsNet() {
  const [isOssrsNet, setIsOssrsNet] = React.useState(false);
  React.useEffect(() => {
    if (window.location.hostname.indexOf('ossrs.net') >= 0) {
      setIsOssrsNet(true);
    }
  }, [setIsOssrsNet]);
  return isOssrsNet;
}

function App() {
  const [btnClassName, setBtnClassName] = React.useState('StaticButton');
  const [loading, setLoading] = React.useState(false);
  const mediaRecorderRef = React.useRef(null);
  const audioChunkRef = React.useRef([]);
  const [started, setStarted] = React.useState(false);
  const audioPlayerRef = React.useRef(null);
  const isMobile = useIsMobile();
  const isOssrsNet = useIsOssrsNet();

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

  const startRecording = React.useCallback(async () => {
    if (mediaRecorderRef.current) return;

    setBtnClassName('DynamicButton');
    writeLongLog("===========================");
    writeShortLog("=============");

    const stream = await new Promise(resolve => {
      navigator.mediaDevices.getUserMedia(
        { audio: true }
      ).then((stream) => {
        resolve(stream);
      }).catch(error => alert(`Device error: ${error}`));
    });

    // See https://www.sitelint.com/lab/media-recorder-supported-mime-type/
    mediaRecorderRef.current = new MediaRecorder(stream);
    mediaRecorderRef.current.addEventListener("dataavailable", ({ data }) => {
      audioChunkRef.current.push(data);
      writeLongLog(`Event: dataavailable ${data.size} bytes`);
    });

    mediaRecorderRef.current.start();
    writeLongLog(`Event: Recording started`);
  }, [writeLongLog, writeShortLog, setBtnClassName, mediaRecorderRef, audioChunkRef]);

  const stopRecording = React.useCallback(async () => {
    if (!mediaRecorderRef.current) return;

    await new Promise(resolve => {
      mediaRecorderRef.current.addEventListener("stop", () => {
        const stream = mediaRecorderRef.current.stream;
        stream.getTracks().forEach(track => track.stop());
        setTimeout(resolve, 300);
      });

      writeLongLog(`Event: Recorder stop, chunks=${audioChunkRef.current.length}, state=${mediaRecorderRef.current.state}`);
      mediaRecorderRef.current.stop();
    });

    setLoading(true);
    writeLongLog(`Event: Recoder stopped, chunks=${audioChunkRef.current.length}`);

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
      mediaRecorderRef.current = null;
    }
  }, [writeLongLog, writeShortLog, setBtnClassName, mediaRecorderRef, audioChunkRef, setLoading, audioPlayerRef]);

  const onStart = React.useCallback(async () => {
    writeLongLog("Start the app");
    writeShortLog("Start the app");

    await new Promise(resolve => {
      audioPlayerRef.current.src = "/api/ai-talk/examples/hello.aac";
      audioPlayerRef.current.play()
        .catch(error => alert(`Play error: ${error}`));
      audioPlayerRef.current.addEventListener('ended', () => {
        resolve();
      });
    });

    const stream = await new Promise(resolve => {
      navigator.mediaDevices.getUserMedia(
        {audio: true}
      ).then((stream) => {
        const recorder = new MediaRecorder(stream);

        const audioChunks = [];
        recorder.addEventListener("dataavailable", ({data}) => {
          audioChunks.push(data);
        });
        recorder.addEventListener("stop", async () => {
          writeLongLog(`Start: Microphone stop, chunks=${audioChunks.length}, state=${recorder.state}`);
          resolve(stream);
        });

        recorder.start();
        setTimeout(() => {
          recorder.stop();
          writeLongLog(`Start: Microphone state is ${recorder.state}`);
        }, 100);
      }).catch(error => alert(`Open microphone error: ${error}`));
    });

    await new Promise(resolve => {
      stream.getTracks().forEach(track => track.stop());
      setTimeout(() => {
        resolve();
      }, 200);
    });

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

  React.useEffect(() => {
    document.title = "AI Talk";
  }, []);

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
    <p><audio ref={audioPlayerRef} controls={true} hidden={false} /></p>
    <p>
      <button onClick={(e) => {
        setShowShortLogs(!showShortLogs);
      }}>{showShortLogs ? 'Detail Logs' : 'Short Logs'}</button> &nbsp;
      <button onClick={(e) => {
        audioPlayerRef.current.src = "/api/ai-talk/examples/example.aac";
        audioPlayerRef.current.play();
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
    {isOssrsNet && <img src="https://ossrs.net/gif/v1/sls.gif?site=ossrs.net&path=/stat/ai-talk"/>}
  </div>);
}

export default App;
