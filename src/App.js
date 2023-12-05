import React from 'react';
import './App.css';

function App() {
  const [btnClassName, setBtnClassName] = React.useState('StaticButton');

  const [userLog, setUserLog] = React.useState();
  const writeLog = React.useCallback((msg) => {
    const date = new Date();
    const hours = date.getHours().toString().padStart(2, '0');
    const minutes = date.getMinutes().toString().padStart(2, '0');
    const seconds = date.getSeconds().toString().padStart(2, '0');

    setUserLog(`[${hours}:${minutes}:${seconds}]: ${msg}`);
    console.log(`[${hours}:${minutes}:${seconds}]: ${msg}`);
  }, [setUserLog]);

  const mediaRecorderRef = React.useRef(null);
  const audioChunkRef = React.useRef([]);

  const startRecording = React.useCallback(() => {
    if (mediaRecorderRef.current) return;

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

      const audioBlob = new Blob(audioChunkRef.current);
      const formData = new FormData();
      formData.append('file', audioBlob, 'audio.aac');
      fetch('/api/gpt-microphone/upload/', {
        method: 'POST',
        body: formData,
      }).then(response => {
        return response.text();
      }).then((data) => {
        console.log(`Upload success: ${data}`);
      }).catch(error => alert(error));

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
          setBtnClassName('DynamicButton');
          writeLog(`Event: onTouchStart`);
          startRecording();
        }}
        onTouchEnd={(e) => {
          setBtnClassName('StaticButton');
          writeLog(`Event: onTouchEnd`);
          stopRecording();
        }}
        onKeyDown={(e) => {
          if (e.key !== 'r' && e.key !== '\\') return;
          setBtnClassName('DynamicButton');
          writeLog(`Event: onKeyDown ${e.key}`);
          startRecording();
        }}
        onKeyUp={(e) => {
          if (e.key !== 'r' && e.key !== '\\') return;
          setBtnClassName('StaticButton');
          writeLog(`Event: onKeyUp ${e.key}`);
          stopRecording();
        }}
        className={btnClassName}
      ></button>
      <p>{userLog}</p>
    </header>
  </div>);
}

export default App;
