import React from 'react';
import './App.css';
import {RobotConfig, useIsMobile, useIsOssrsNet, buildLog} from "./utils";

function App() {
  // User selected robot.
  const [robot, setRobot] = React.useState(null);
  const [stageUUID, setStageUUID] = React.useState(null);
  // Whether robot is ready, user're allowd to talk with AI.
  const [robotReady, setRobotReady] = React.useState(false);

  // The player ref, to access the audio player.
  const playerRef = React.useRef(null);
  // The log and debug panel.
  const [info, verbose, showVerboseLogs, logPanel] = useDebugPanel(playerRef);

  // User start a stage.
  const onStartStage = React.useCallback(async (uuid, robot) => {
    verbose("Start the app");

    setStageUUID(uuid);
    setRobot(robot);

    // Play the welcome audio.
    await new Promise(resolve => {
      verbose(`Start: Play hello welcome audio`);

      playerRef.current.src = `/api/ai-talk/examples/${robot.voice}?sid=${uuid}`;
      playerRef.current.play()
        .catch(error => alert(`Play error: ${error}`));
      playerRef.current.addEventListener('ended', () => {
        resolve();
      });
    });

    setRobotReady(true);
    info(`Stage started, AI is ready`);
    verbose(`Stage started, AI is ready, sid=${uuid}`);
  }, [verbose, playerRef, setRobot, setStageUUID, robotReady]);

  return <>
    <div><audio ref={playerRef} controls={true} hidden={!showVerboseLogs} /></div>
    {!robot && <SelectRobot {...{info, verbose, onStartStage}}/>}
    {robot && logPanel}
    {robot && <AppImpl {...{
      info, verbose, robot, robotReady, stageUUID, playerRef,
    }}/>}
  </>;
}

function useDebugPanel({playerRef}) {
  // Verbose(detail) and info(summary) logs, show in the log panel.
  const [showVerboseLogs, setShowVerboseLogs] = React.useState(false);
  const [verboseLogs, setVerboseLogs] = React.useState([]);
  const [infoLogs, setInfoLogs] = React.useState([]);

  // The refs, about the logs.
  const ref = React.useRef({
    verboseLogs: [],
    infoLogs: [],
  });

  // Write a summary or info log, which is important but short message for user.
  const info = React.useCallback((msg) => {
    ref.current.infoLogs = [...ref.current.infoLogs, msg];
    setInfoLogs(ref.current.infoLogs);
  }, [ref, setInfoLogs]);

  // Write a verbose or detail log, which is very detail for debugging for developer.
  const verbose = React.useCallback((msg) => {
    ref.current.verboseLogs = [...ref.current.verboseLogs, buildLog(msg)];
    setVerboseLogs(ref.current.verboseLogs);
  }, [ref, setVerboseLogs]);

  // Scroll the log panel.
  const logPanelRef = React.useRef(null);
  React.useEffect(() => {
    logPanelRef?.current?.scrollIntoView({ behavior: "smooth", block: "end" });
  }, [verboseLogs, infoLogs, logPanelRef]);

  // User click the welcome audio button.
  const onClickWelcomeAudio = React.useCallback(() => {
    verbose(`Play example aac audio`);
    playerRef.current.src = `/api/ai-talk/examples/hello.aac`;
    playerRef.current.play();
  }, [verbose, playerRef]);

  return [info, verbose, showVerboseLogs, <React.Fragment>
    <div style={{textAlign: 'right', padding: '10px'}}>
      <button onClick={(e) => {
        verbose(`Set debugging to ${!showVerboseLogs}`);
        setShowVerboseLogs(!showVerboseLogs);
      }}>{!showVerboseLogs ? 'Debug' : 'Quit debugging'}</button> &nbsp;
      {showVerboseLogs && <>
        <button onClick={(e) => {
          onClickWelcomeAudio();
        }}>Welcome audio</button> &nbsp;
        <a href="https://github.com/winlinvip/ai-talk/discussions" target='_blank'>Help me!</a>
      </>}
    </div>
    <div className='LogPanel'>
      <ul>
        {showVerboseLogs && verboseLogs.map((log, index) => {
          return (<li key={index}>{log}</li>);
        })}
        {!showVerboseLogs && infoLogs.map((log, index) => {
          const you = log.indexOf('You:') === 0;
          const bot = log.indexOf('Bot:') === 0;
          const color = you ? 'darkgreen' : (bot ? 'darkblue' : '');
          const fontWeight = you ? 'bold' : 'normal';
          return (<li key={index} style={{color,fontWeight}}>{log}</li>);
        })}
      </ul>
      <div style={{ float:"left", clear: "both" }} ref={logPanelRef}/>
    </div>
  </React.Fragment>];
}

function SelectRobot({info, verbose, onStartStage}) {
  // Whether system is booting.
  const [booting, setBooting] = React.useState(true);
  // Whether system checking, such as should be HTTPS.
  const [allowed, setAllowed] = React.useState(false);
  // Whether we're loading the page and request permission.
  const [loading, setLoading] = React.useState(true);

  // The available robots for user to select.
  const [robots, setRobots] = React.useState([]);
  const [robot, setRobot] = React.useState(RobotConfig.load());
  const [uuid, setUUID] = React.useState(null);

  // The application is started now.
  React.useEffect(() => {
    // Only allow localhost or https to access microphone.
    const isLo = window.location.hostname === 'localhost' || window.location.hostname === '127.0.0.1';
    const isHttps = window.location.protocol === 'https:';
    const securityAllowed = isLo || isHttps;
    securityAllowed || info(`App started, allowed=${securityAllowed}`);
    verbose(`App started, allowed=${securityAllowed}, lo=${isLo}, https=${isHttps}`);
    if (!securityAllowed) return;

    // Try to open the microphone to request permission.
    new Promise(resolve => {
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
          // Stop the microphone.
          verbose(`Start: Microphone ok, chunks=${audioChunks.length}, state=${recorder.state}`);
          stream.getTracks().forEach(track => track.stop());
          setTimeout(() => {
            verbose(`Start: Microphone test ok.`);
            resolve();
          }, 50);
        });

        recorder.start();
        setTimeout(() => {
          recorder.stop();
          verbose(`Start: Microphone stopping, state is ${recorder.state}`);
        }, 30);
      }).catch(error => alert(`Open microphone error: ${error}`));
    }).then(() => {
      setAllowed(true);
      setBooting(false);
    });
  }, [setAllowed, setBooting]);

  // Request server to create a new stage.
  React.useEffect(() => {
    if (!allowed) return;

    verbose(`Start: Create a new stage`);

    fetch('/api/ai-talk/start/', {
      method: 'POST',
    }).then(response => {
      return response.json();
    }).then((data) => {
      verbose(`Start: Create stage success: ${data.data.sid}, ${data.data.robots.length} robots`);
      setUUID(data.data.sid);
      setRobots(data.data.robots);

      const config = RobotConfig.load();
      if (config) {
        const robot = data.data.robots.find(robot => robot.uuid === config.uuid);
        if (robot) {
          setRobot(robot);
          info(`Use previous robot ${robot.label}`);
          verbose(`Use previous robot ${robot.label} ${robot.uuid}`);
        }
      }

      setLoading(false);
    }).catch((error) => alert(`Create stage error: ${error}`));
  }, [setLoading, setRobots, setRobot, allowed, setUUID]);

  // User select a robot.
  const onSelectRobot = React.useCallback((e) => {
    if (!e.target.value) return;
    const robot = robots.find(robot => robot.uuid === e.target.value);
    setRobot(robot);
    RobotConfig.save(robot);
    info(`Change to robot ${robot.label}`);
    verbose(`Change to robot ${robot.label} ${robot.uuid}`);
  }, [robots, setRobot]);

  return <div className='SelectRobotDiv'>
    {!booting && !allowed && <p style={{color: "red"}}>
      Error: Only allow localhost or https to access microphone.
    </p>}
    <p>
      {robots?.length ? <React.Fragment>
        Assistant: &nbsp;
        <select className='SelectRobot' defaultValue={robot?.uuid}
                onChange={(e) => onSelectRobot(e)}>
          <option value=''>Please select a robot</option>
          {robots.map(robot => {
            return <option key={robot.uuid} value={robot.uuid}>{robot.label}</option>;
          })}
        </select> &nbsp;
      </React.Fragment> : ''}
    </p>
    <p>
      {!loading && robot && <button className='StartButton' onClick={(e) => {
        onStartStage(uuid, robot);
      }}>Next</button>}
    </p>
  </div>;
}

function AppImpl({info, verbose, robot, robotReady, stageUUID, playerRef}) {
  const isMobile = useIsMobile();
  const isOssrsNet = useIsOssrsNet();

  // Whether user is having a conversation with AI, from user speak, util AI speak.
  const [working, setWorking] = React.useState(false);
  // Whether microphone is really working, when state change to active.
  const [micWorking, setMicWorking] = React.useState(false);
  // Whether AI is processing the user input and generating the response.
  const [processing, setProcessing] = React.useState(false);

  // The refs, about the logs and audio chunks model.
  const ref = React.useRef({
    isRecording: false,
    stopHandler: null,
    mediaRecorder: null,
    audioChunks: [],
  });

  // User start a conversation, by recording input.
  const startRecording = React.useCallback(async () => {
    if (!robotReady) return;
    if (ref.current.stopHandler) clearTimeout(ref.current.stopHandler);
    if (ref.current.mediaRecorder) return;
    if (ref.current.isRecording) return;
    ref.current.isRecording = true;

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

    // See https://developer.mozilla.org/en-US/docs/Web/API/MediaRecorder#events
    ref.current.mediaRecorder.addEventListener("start", () => {
      verbose(`Event: Recording start to record`);
      setMicWorking(true);
    });

    ref.current.mediaRecorder.addEventListener("dataavailable", ({ data }) => {
      ref.current.audioChunks.push(data);
      verbose(`Event: Device dataavailable event ${data.size} bytes`);
    });

    ref.current.mediaRecorder.start();
    verbose(`Event: Recording started`);
  }, [info, verbose, robotReady, ref, setMicWorking, setWorking]);

  // User click stop button, we delay some time to allow cancel the stopping event.
  const stopRecording = React.useCallback(async () => {
    if (!robotReady) return;

    const stopRecordingImpl = async () => {
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

      setMicWorking(false);
      setProcessing(true);
      verbose(`Event: Recoder stopped, chunks=${ref.current.audioChunks.length}`);

      try {
        // Upload the user input audio to the server.
        const requestUUID = await new Promise((resolve, reject) => {
          verbose(`ASR: Uploading ${ref.current.audioChunks.length} chunks, robot=${robot.uuid}`);
          const audioBlob = new Blob(ref.current.audioChunks);
          ref.current.audioChunks = [];

          // It can be aac or ogg codec.
          const formData = new FormData();
          formData.append('file', audioBlob, 'input.audio');

          fetch(`/api/ai-talk/upload/?sid=${stageUUID}&robot=${robot.uuid}`, {
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
          verbose(`TTS: Requesting ${requestUUID} response audios, rid=${requestUUID}`);
          let audioSegmentUUID = null;
          while (!audioSegmentUUID) {
            const resp = await new Promise((resolve, reject) => {
              fetch(`/api/ai-talk/query/?sid=${stageUUID}&rid=${requestUUID}`, {
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
            verbose(`TTS: All audios are played, rid=${requestUUID}`);
            verbose("=============");
            break;
          }

          // Play the AI generated audio.
          await new Promise(resolve => {
            const url = `/api/ai-talk/tts/?sid=${stageUUID}&rid=${requestUUID}&asid=${audioSegmentUUID}`;
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
            fetch(`/api/ai-talk/remove/?sid=${stageUUID}&rid=${requestUUID}&asid=${audioSegmentUUID}`, {
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
        ref.current.isRecording = false;
      }
    };

    if (ref.current.stopHandler) clearTimeout(ref.current.stopHandler);
    ref.current.stopHandler = setTimeout(() => {
      stopRecordingImpl();
    }, 800);
  }, [info, verbose, playerRef, stageUUID, robot, robotReady, ref, setProcessing, setWorking]);

  // Setup the keyboard event, for PC browser.
  React.useEffect(() => {
    if (!robotReady) return;

    const handleKeyDown = (e) => {
      if (processing) return;
      if (e.key !== 'r' && e.key !== '\\') return;
      startRecording();
    };
    const handleKeyUp = (e) => {
      if (processing) return;
      if (e.key !== 'r' && e.key !== '\\') return;
      stopRecording();
    };

    window.addEventListener('keydown', handleKeyDown);
    window.addEventListener('keyup', handleKeyUp);
    return () => {
      window.removeEventListener('keydown', handleKeyDown);
      window.removeEventListener('keyup', handleKeyUp);
    };
  }, [robotReady, startRecording, stopRecording, processing]);

  return <>
    <button className="App-header"
            onTouchStart={startRecording}
            onTouchEnd={stopRecording}
            disabled={!robotReady || processing}>
      <label className={!working ? 'StaticButton' : processing ? 'ProcessingButton' : micWorking ? 'RecordingButton' : 'DynamicButton'}>
        {processing ? 'Processing' : working ? '' : (isMobile ? 'Press to talk' : 'Press the R key')}
      </label>
    </button>
    {isOssrsNet && <img className='LogGif' src="https://ossrs.net/gif/v1/sls.gif?site=ossrs.net&path=/stat/ai-talk" alt=''/>}
  </>;
}

export default App;
