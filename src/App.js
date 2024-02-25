import React from 'react';
import './App.css';
import {useIsMobile, useIsOssrsNet} from "./utils";
import {useDebugPanel} from "./debugPanel";
import {useRobotInitiator} from "./robotInitiator";

const timeoutWaitForLastVoice = 700;
const durationRequiredUserInput = 600;

function App() {
  // The player ref, to access the audio player.
  const playerRef = React.useRef(null);
  // The log and debug panel.
  const [info, verbose, showVerboseLogs, logPanel] = useDebugPanel(playerRef);
  // The robot initialize and select UI.
  const [robot, stageUUID, robotReady, robotPanel] = useRobotInitiator(info, verbose, playerRef);

  return <>
    <div><audio ref={playerRef} controls={true} hidden={!showVerboseLogs} /></div>
    {robot ? logPanel : robotPanel}
    {robot && <AppImpl {...{info, verbose, robot, robotReady, stageUUID, playerRef}}/>}
  </>;
}

function AppImpl({info, verbose, robot, robotReady, stageUUID, playerRef}) {
  const isOssrsNet = useIsOssrsNet();
  const isMobile = useIsMobile();
  const [statLink, setStatLink] = React.useState(null);

  // Whether user is press the microhpone and talking.
  const [talking, setTalking] = React.useState(false);
  // Whether microphone is really working, when state change to active.
  const [micWorking, setMicWorking] = React.useState(false);
  // Whether AI is processing the user input and generating the response.
  const [processing, setProcessing] = React.useState(false);

  // The refs, about the logs and audio chunks model.
  const ref = React.useRef({
    count: 0,
    isRecording: false,
    recordStarttime: null,
    stopHandler: null,
    mediaStream: null,
    mediaRecorder: null,
    audioChunks: [],
  });

  // When robot is ready, open the microphone ASAP to accept user input.
  React.useEffect(() => {
    if (!robotReady) return;
    if (ref.current.mediaStream) return;

    verbose(`Robot is ready, open microphone.`)
    navigator.mediaDevices.getUserMedia(
      { audio: true }
    ).then((stream) => {
      ref.current.mediaStream = stream;
      verbose(`Robot is ready, microphone opened.`);
    }).catch(error => alert(`Device error: ${error}`));
  }, [robotReady, ref]);

  // User start a conversation, by recording input.
  const startRecording = React.useCallback(async () => {
    if (!robotReady) return;
    if (!ref.current.mediaStream) return;
    if (ref.current.stopHandler) clearTimeout(ref.current.stopHandler);
    if (ref.current.mediaRecorder) return;
    if (ref.current.isRecording) return;
    ref.current.recordStarttime = new Date();
    ref.current.isRecording = true;
    ref.current.count += 1;

    setTalking(true);
    verbose("=============");

    // The stream is already opened when robot ready, or all answers are played.
    // See https://www.sitelint.com/lab/media-recorder-supported-mime-type/
    ref.current.mediaRecorder = new MediaRecorder(ref.current.mediaStream);
    ref.current.mediaStream = null;

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
  }, [info, verbose, robotReady, ref, setMicWorking, setTalking]);

  // User click stop button, we delay some time to allow cancel the stopping event.
  const stopRecording = React.useCallback(async () => {
    if (!robotReady) return;

    const processUserInput = async(userMayInput) => {
      // End conversation, for stat the elapsed time cost accurately.
      await new Promise((resolve, reject) => {
        fetch(`/api/ai-talk/conversation/?sid=${stageUUID}&robot=${robot.uuid}&umi=${userMayInput}`, {
          method: 'POST',
        }).then(response => {
          return response.json();
        }).then((data) => {
          verbose(`TTS: Conversation started`);
          resolve();
        }).catch(error => reject(error));
      });

      // Upload the user input audio to the server.
      const requestUUID = await new Promise((resolve, reject) => {
        verbose(`ASR: Uploading ${ref.current.audioChunks.length} chunks, robot=${robot.uuid}`);
        const audioBlob = new Blob(ref.current.audioChunks);
        ref.current.audioChunks = [];

        // It can be aac or ogg codec.
        const formData = new FormData();
        formData.append('file', audioBlob, 'input.audio');

        fetch(`/api/ai-talk/upload/?sid=${stageUUID}&robot=${robot.uuid}&umi=${userMayInput}`, {
          method: 'POST',
          body: formData,
        }).then(response => {
          return response.json();
        }).then((data) => {
          verbose(`ASR: Upload success: ${data.data.rid} ${data.data.asr}`);
          info('user', `${data.data.asr}`);
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
                info('bot', `${data.data.tts}`);
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
    };

    const stopRecordingImpl = async () => {
      if (!ref.current.mediaRecorder) return;

      try {
        const userMayInput = new Date() - ref.current.recordStarttime - timeoutWaitForLastVoice;
        verbose(`Event: User stop record, duration=${userMayInput}ms, state=${ref.current.mediaRecorder.state}`);

        await new Promise(resolve => {
          ref.current.mediaRecorder.addEventListener("stop", () => {
            const stream = ref.current.mediaRecorder.stream;
            stream.getTracks().forEach(track => track.stop());
            setTimeout(resolve, 30);
          });

          verbose(`Event: Recorder stop, chunks=${ref.current.audioChunks.length}, state=${ref.current.mediaRecorder.state}`);
          ref.current.mediaRecorder.stop();
        });

        info('user', ''); // Insert a empty line to show loading of user input.
        setTalking(false);
        setMicWorking(false);
        setProcessing(true);
        verbose(`Event: Recoder stopped, chunks=${ref.current.audioChunks.length}`);

        if (userMayInput < durationRequiredUserInput) {
          info('sys', `System: You didn't say anything!`);
          alert(`Warning: You didn't say anything!`);
        } else {
          try {
            await processUserInput(userMayInput);
          } catch (e) {
            info('sys', `System: Server error ${e}`);
            info('sys', `System: Please try again.`);
            alert(`System: Server error ${e}`);
          }
        }

        // Reopen the microphone.
        verbose(`Robot is ready, open microphone.`)
        new Promise((resolve, reject) => {
          navigator.mediaDevices.getUserMedia(
            { audio: true }
          ).then((stream) => {
            ref.current.mediaStream = stream;
            verbose(`All audios is played, microphone opened.`);
            resolve();
          }).catch(error => reject(error));
        });
      } catch (e) {
        alert(e);
      } finally {
        setProcessing(false);
        ref.current.mediaRecorder = null;
        ref.current.isRecording = false;
      }
    };

    if (ref.current.stopHandler) clearTimeout(ref.current.stopHandler);
    ref.current.stopHandler = setTimeout(() => {
      stopRecordingImpl();
    }, timeoutWaitForLastVoice);
  }, [info, verbose, playerRef, stageUUID, robot, robotReady, ref, setProcessing, setTalking, setMicWorking]);

  // Setup the keyboard event, for PC browser.
  React.useEffect(() => {
    if (!robotReady) return;

    const handleKeyDown = (e) => {
      if (processing) return;
      if (e.key !== 'r' && e.key !== '\\' && e.key !== ' ') return;
      startRecording();
    };
    const handleKeyUp = (e) => {
      if (processing) return;
      if (e.key !== 'r' && e.key !== '\\' && e.key !== ' ') return;
      stopRecording();
    };

    window.addEventListener('keydown', handleKeyDown);
    window.addEventListener('keyup', handleKeyUp);
    return () => {
      window.removeEventListener('keydown', handleKeyDown);
      window.removeEventListener('keyup', handleKeyUp);
    };
  }, [robotReady, startRecording, stopRecording, processing]);

  React.useEffect(() => {
    if (!isOssrsNet) return;
    setStatLink(`https://ossrs.net/gif/v1/sls.gif?site=ossrs.net&path=/stat/ai-talk/${robot.uuid}`);
  }, [isOssrsNet, setStatLink]);

  return <>
    <div className="App-header"
            onTouchStart={startRecording}
            onTouchEnd={stopRecording}
            disabled={!robotReady || processing}>
      {robotReady && !processing && <div>
        <div className='mc-text'>
          {!talking ? <span>{isMobile ? 'Press to talk!' : 'Press the R key or SPACE to talk!'}</span> : <span>&nbsp;</span>}
        </div>
        <div className={micWorking ? 'gn-active' : 'gn'}>
          <div className='mc'></div>
        </div>
      </div>}
    </div>
    {statLink && <img className='LogGif' src={statLink} alt=''/>}
  </>;
}

export default App;
