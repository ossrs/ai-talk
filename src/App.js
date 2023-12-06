import React from 'react';
import './App.css';

function App() {
  const [btnClassName, setBtnClassName] = React.useState('StaticButton');
  const [loading, setLoading] = React.useState(false);
  const mediaRecorderRef = React.useRef(null);
  const audioChunkRef = React.useRef([]);
  const [logRenders, setLogRenders] = React.useState([]);
  const logs = React.useRef([]);

  const writeLog = React.useCallback((msg) => {
    const date = new Date();
    const hours = date.getHours().toString().padStart(2, '0');
    const minutes = date.getMinutes().toString().padStart(2, '0');
    const seconds = date.getSeconds().toString().padStart(2, '0');

    const log = `[${hours}:${minutes}:${seconds}]: ${msg}`;
    console.log(log);

    logs.current = [log, ...logs.current];
    setLogRenders(logs.current);
  }, [logs, setLogRenders]);

  React.useEffect(() => {
    writeLog('App started');
  }, [writeLog]);

  const startRecording = React.useCallback(() => {
    if (mediaRecorderRef.current) return;

    setBtnClassName('DynamicButton');
    writeLog("===========================");

    navigator.mediaDevices.getUserMedia(
      { audio: true }
    ).then((stream) => {
      // See https://www.sitelint.com/lab/media-recorder-supported-mime-type/
      mediaRecorderRef.current = new MediaRecorder(stream);
      mediaRecorderRef.current.addEventListener("dataavailable", ({ data }) => {
        audioChunkRef.current.push(data);
        writeLog(`Event: dataavailable ${data.size} bytes`);
      });
      mediaRecorderRef.current.start();
      writeLog(`Event: Recording started`);
    }).catch(error => alert(error));
  }, [writeLog]);

  const stopRecording = React.useCallback(() => {
    if (!mediaRecorderRef.current) return;

    mediaRecorderRef.current.addEventListener("stop", async () => {
      writeLog(`Event: stopping, ${audioChunkRef.current.length} chunks`);

      setLoading(true);

      try {
        // Upload the user input audio to the server.
        const uuid = await new Promise((resolve, reject) => {
          writeLog(`ASR: Uploading ${audioChunkRef.current.length} chunks`);
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
            writeLog(`ASR: Upload success: ${data.data.uuid} ${data.data.asr}`);
            resolve(data.data.uuid);
          }).catch((error) => reject(error));
        });

        // Get the AI generated audio from the server.
        while (true) {
          writeLog(`TTS: Requesting ${uuid} response audios`);
          let readyUUID = null;
          while (!readyUUID) {
            const resp = await new Promise((resolve, reject) => {
              fetch(`/api/ai-talk/question/?rid=${uuid}`, {
                method: 'POST',
              }).then(response => {
                return response.json();
              }).then((data) => {
                if (data?.data?.uuid) writeLog(`TTS: Audio ready: ${data.data.uuid} ${data.data.tts}`);
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
            writeLog(`TTS: All audios are played.`);
            writeLog("===========================");
            break;
          }

          // Play the AI generated audio.
          await new Promise(resolve => {
            const url = `/api/ai-talk/tts/?rid=${uuid}&uuid=${readyUUID}`;
            writeLog(`TTS: Playing ${url}`);
            const audio = new Audio(url);
            audio.loop = false;
            audio.addEventListener('ended', () => {
              writeLog(`TTS: Played ${url} done.`);
              resolve();
            });
            audio.play();
          });

          // Remove the AI generated audio.
          await new Promise((resolve, reject) => {
            fetch(`/api/ai-talk/remove/?rid=${uuid}&uuid=${readyUUID}`, {
              method: 'POST',
            }).then(response => {
              return response.json();
            }).then((data) => {
              writeLog(`TTS: Audio removed: ${readyUUID}`);
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

    writeLog(`Event: stop, ${audioChunkRef.current.length} chunks`);
    mediaRecorderRef.current.stop();
    mediaRecorderRef.current = null;
  }, [writeLog]);

  return (<div className="App">
    <header className="App-header">
      <button
        onTouchStart={(e) => {
          startRecording();
        }}
        onTouchEnd={(e) => {
          setTimeout(() => {
            stopRecording();
          }, 800);
        }}
        onKeyDown={(e) => {
          if (e.key !== 'r' && e.key !== '\\') return;
          startRecording();
        }}
        onKeyUp={(e) => {
          if (e.key !== 'r' && e.key !== '\\') return;
          setTimeout(() => {
            stopRecording();
          }, 800);
        }}
        className={btnClassName}
        disabled={loading}
      ></button>
    </header>
    <ul className='LogPanel'>
      {logRenders.map((log, index) => {
        return (<li key={index}>{log}</li>);
      })}
    </ul>
    <button onClick={(e) => {
      const audio = new Audio("/api/ai-talk/examples/example");
      audio.loop = false;
      audio.play();
    }}>Example</button> &nbsp;
    <button onClick={(e) => {
      const audio = new Audio("/api/ai-talk/examples/example.aac");
      audio.loop = false;
      audio.play();
    }}>Example aac</button> &nbsp;
    <button onClick={(e) => {
      const audio = new Audio("/api/ai-talk/examples/example.opus");
      audio.loop = false;
      audio.play();
    }}>Example opus</button> &nbsp;
    <button onClick={(e) => {
      const audio = new Audio("/api/ai-talk/examples/example.mp3");
      audio.loop = false;
      audio.play();
    }}>Example mp3</button> &nbsp;
  </div>);
}

export default App;
