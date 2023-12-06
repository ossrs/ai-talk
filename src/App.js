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

    mediaRecorderRef.current.addEventListener("stop", () => {
      writeLog(`Event: stopping, ${audioChunkRef.current.length} chunks`);

      setLoading(true);
      writeLog(`Uploading ${audioChunkRef.current.length} chunks`);

      const audioBlob = new Blob(audioChunkRef.current);
      const formData = new FormData();
      // It can be aac or ogg codec.
      formData.append('file', audioBlob, 'input.audio');
      fetch('/api/ai-talk/upload/', {
        method: 'POST',
        body: formData,
      }).then(response => {
        return response.json();
      }).then((data) => {
        writeLog(`Upload success: ${JSON.stringify(data)}`);
      }).catch(error => alert(error)).finally(() => {
        setLoading(false);
        setBtnClassName('StaticButton');
      });

      audioChunkRef.current = [];
      writeLog(`Event: Recording stopped`);
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
  </div>);
}

export default App;
