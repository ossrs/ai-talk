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

function buildLog(msg) {
  const date = new Date();
  const hours = date.getHours().toString().padStart(2, '0');
  const minutes = date.getMinutes().toString().padStart(2, '0');
  const seconds = date.getSeconds().toString().padStart(2, '0');

  const log = `[${hours}:${minutes}:${seconds}]: ${msg}`;
  console.log(log);
  return log;
}

function App() {
  const isMobile = useIsMobile();
  const isOssrsNet = useIsOssrsNet();

  // Whether user click the start, we're trying to start the stage.
  const [starting, setStarting] = React.useState(false);
  // Whether stage is starged, user're allowd to talk with AI.
  const [started, setStarted] = React.useState(false);
  // Whether user is having a conversation with AI, from user speak, util AI speak.
  const [working, setWorking] = React.useState(false);
  // Whether AI is processing the user input and generating the response.
  const [processing, setProcessing] = React.useState(false);

  // Verbose(detail) and info(summary) logs, show in the log panel.
  const [showVerboseLogs, setShowVerboseLogs] = React.useState(false);
  const [verboseLogs, setVerboseLogs] = React.useState([]);
  const [infoLogs, setInfoLogs] = React.useState([]);

  // The refs, about the player and under layer record and audio chunks model.
  const playerRef = React.useRef(null);
  const ref = React.useRef({
    mediaRecorder: null,
    audioChunks: [],
    verboseLogs: [],
    infoLogs: [],
  });

  // Write a summary or info log, which is important but short message for user.
  const info = React.useCallback((msg) => {
    ref.current.infoLogs = [msg, ...ref.current.infoLogs];
    setInfoLogs(ref.current.infoLogs);
  }, [ref, setInfoLogs]);

  // Write a verbose or detail log, which is very detail for debugging for developer.
  const verbose = React.useCallback((msg) => {
    ref.current.verboseLogs = [buildLog(msg), ...ref.current.verboseLogs];
    setVerboseLogs(ref.current.verboseLogs);
  }, [ref, setVerboseLogs]);

  // Setup the website title.
  React.useEffect(() => {
    document.title = "AI Talk";
  }, []);

  // Setup the keyboard event, for PC browser.
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

  // The application is started now.
  React.useEffect(() => {
    verbose('App started');
    info('App started');
  }, [info, verbose]);

  // User start a stage.
  const onStartStage = React.useCallback(async () => {
    try {
      setStarting(true);
      verbose("Start the app");

      await new Promise(resolve => {
        verbose(`Start: Play hello welcome audio`);
        playerRef.current.src = "/api/ai-talk/examples/hello.aac";
        playerRef.current.play()
          .catch(error => alert(`Play error: ${error}`));
        playerRef.current.addEventListener('ended', () => {
          resolve();
        });
      });

      const stageUUID = await new Promise((resolve, reject) => {
        verbose(`Start: Create a new stage`);

        fetch('/api/ai-talk/start/', {
          method: 'POST',
        }).then(response => {
          return response.json();
        }).then((data) => {
          verbose(`Start: Create stage success: ${data.data.sid}`);
          resolve(data.data.sid);
        }).catch((error) => reject(error));
      });

      const stream = await new Promise(resolve => {
        verbose(`Start: Open microphone`);

        navigator.mediaDevices.getUserMedia(
          {audio: true}
        ).then((stream) => {
          verbose(`Start: Microphone opened, try to record`);
          const recorder = new MediaRecorder(stream);

          const audioChunks = [];
          recorder.addEventListener("dataavailable", ({data}) => {
            audioChunks.push(data);
          });
          recorder.addEventListener("stop", async () => {
            verbose(`Start: Microphone ok, chunks=${audioChunks.length}, state=${recorder.state}`);
            resolve(stream);
          });

          recorder.start();
          setTimeout(() => {
            recorder.stop();
            verbose(`Start: Microphone stopping, state is ${recorder.state}`);
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
      info(`Stage started, AI is ready`);
      verbose(`Stage started, AI is ready, sid=${stageUUID}`);
    } finally {
      setStarting(false);
    }
  }, [verbose, info, setStarted, playerRef]);

  // User start a conversation, by recording input.
  const startRecording = React.useCallback(async () => {
    if (ref.current.mediaRecorder) return;

    setWorking(true);
    verbose("=============");
    info("=============");

    const stream = await new Promise(resolve => {
      navigator.mediaDevices.getUserMedia(
        { audio: true }
      ).then((stream) => {
        resolve(stream);
      }).catch(error => alert(`Device error: ${error}`));
    });

    // See https://www.sitelint.com/lab/media-recorder-supported-mime-type/
    ref.current.mediaRecorder = new MediaRecorder(stream);
    ref.current.mediaRecorder.addEventListener("dataavailable", ({ data }) => {
      ref.current.audioChunks.push(data);
      verbose(`Event: dataavailable ${data.size} bytes`);
    });

    ref.current.mediaRecorder.start();
    verbose(`Event: Recording started`);
  }, [verbose, info, setWorking, ref]);

  // User stop a conversation, by uploading input and playing response.
  const stopRecording = React.useCallback(async () => {
    if (!ref.current.mediaRecorder) return;

    await new Promise(resolve => {
      ref.current.mediaRecorder.addEventListener("stop", () => {
        const stream = ref.current.mediaRecorder.stream;
        stream.getTracks().forEach(track => track.stop());
        setTimeout(resolve, 300);
      });

      verbose(`Event: Recorder stop, chunks=${ref.current.audioChunks.length}, state=${ref.current.mediaRecorder.state}`);
      ref.current.mediaRecorder.stop();
    });

    setProcessing(true);
    verbose(`Event: Recoder stopped, chunks=${ref.current.audioChunks.length}`);

    try {
      // Upload the user input audio to the server.
      const requestUUID = await new Promise((resolve, reject) => {
        verbose(`ASR: Uploading ${ref.current.audioChunks.length} chunks`);
        const audioBlob = new Blob(ref.current.audioChunks);
        ref.current.audioChunks = [];

        // It can be aac or ogg codec.
        const formData = new FormData();
        formData.append('file', audioBlob, 'input.audio');

        fetch('/api/ai-talk/upload/', {
          method: 'POST',
          body: formData,
        }).then(response => {
          return response.json();
        }).then((data) => {
          verbose(`ASR: Upload success: ${data.data.rid} ${data.data.asr}`);
          info(`You: ${data.data.asr}`);
          resolve(data.data.rid);
        }).catch((error) => reject(error));
      });

      // Get the AI generated audio from the server.
      while (true) {
        verbose(`TTS: Requesting ${requestUUID} response audios`);
        let audioSegmentUUID = null;
        while (!audioSegmentUUID) {
          const resp = await new Promise((resolve, reject) => {
            fetch(`/api/ai-talk/question/?rid=${requestUUID}`, {
              method: 'POST',
            }).then(response => {
              return response.json();
            }).then((data) => {
              if (data?.data?.asid) {
                verbose(`TTS: Audio ready: ${data.data.asid} ${data.data.tts}`);
                info(`Bot: ${data.data.tts}`);
              }
              resolve(data.data);
            }).catch(error => reject(error));
          });

          if (!resp.asid) {
            break;
          }

          if (resp.processing) {
            await new Promise((resolve) => setTimeout(resolve, 300));
            continue;
          }

          audioSegmentUUID = resp.asid;
        }

        // All audios are played.
        if (!audioSegmentUUID) {
          verbose(`TTS: All audios are played.`);
          verbose("===========================");
          break;
        }

        // Play the AI generated audio.
        await new Promise(resolve => {
          const url = `/api/ai-talk/tts/?rid=${requestUUID}&asid=${audioSegmentUUID}`;
          verbose(`TTS: Playing ${url}`);

          const listener = () => {
            playerRef.current.removeEventListener('ended', listener);
            verbose(`TTS: Played ${url} done.`);
            resolve();
          };
          playerRef.current.addEventListener('ended', listener);

          playerRef.current.src = url;
          playerRef.current.play().catch(error => {
            verbose(`TTS: Play ${url} failed: ${error}`);
            resolve();
          });
        });

        // Remove the AI generated audio.
        await new Promise((resolve, reject) => {
          fetch(`/api/ai-talk/remove/?rid=${requestUUID}&asid=${audioSegmentUUID}`, {
            method: 'POST',
          }).then(response => {
            return response.json();
          }).then((data) => {
            verbose(`TTS: Audio removed: ${audioSegmentUUID}`);
            resolve();
          }).catch(error => reject(error));
        });
      }
    } catch (e) {
      alert(e);
    } finally {
      setProcessing(false);
      setWorking(false);
      ref.current.mediaRecorder = null;
    }
  }, [verbose, info, setWorking, ref, setProcessing, playerRef]);

  return (<div className="App">
    <header className="App-header">
      {!started && <button
        disabled={starting} className='StartButton' onClick={(e) => {
          onStartStage();
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
        className={working ? 'DynamicButton' : 'StaticButton'}
        disabled={processing}
      >{isMobile ? 'Press to talk' : 'Press the R key to talk'}</button>}
    </header>
    <p><audio ref={playerRef} controls={true} hidden={false} /></p>
    <p>
      <button onClick={(e) => {
        verbose(`Replay last audio`);
        playerRef.current.play();
      }}>Replay audio</button> &nbsp;
      <button onClick={(e) => {
        setShowVerboseLogs(!showVerboseLogs);
      }}>{!showVerboseLogs ? 'Debugging' : 'Quit debugging'}</button> &nbsp;
      {showVerboseLogs && <React.Fragment>
        <button onClick={(e) => {
          verbose(`Play example aac audio`);
          playerRef.current.src = "/api/ai-talk/examples/hello.aac";
          playerRef.current.play();
        }}>Welcome audio</button> &nbsp;
      </React.Fragment>}
    </p>
    <ul className='LogPanel'>
      {showVerboseLogs && verboseLogs.map((log, index) => {
        return (<li key={index}>{log}</li>);
      })}
      {!showVerboseLogs && infoLogs.map((log, index) => {
        return (<li key={index}>{log}</li>);
      })}
    </ul>
    {isOssrsNet && <img className='LogGif' src="https://ossrs.net/gif/v1/sls.gif?site=ossrs.net&path=/stat/ai-talk"/>}
  </div>);
}

export default App;
