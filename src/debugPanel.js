import React from "react";
import {buildLog} from "./utils";

export function useDebugPanel(playerRef) {
  // Verbose(detail) and info(summary) logs, show in the log panel.
  const [showVerboseLogs, setShowVerboseLogs] = React.useState(false);
  const [verboseLogs, setVerboseLogs] = React.useState([]);
  const [infoLogs, setInfoLogs] = React.useState([]);
  const [sharing, setSharing] = React.useState(false);

  // The refs, about the logs.
  const ref = React.useRef({
    verboseLogs: [],
    infoLogs: [],
  });

  // Write a summary or info log, which is important but short message for user.
  const info = React.useCallback((label, msg) => {
    console.log(`[info] ${buildLog(msg)}`);

    const last = ref.current.infoLogs.length > 0 ? ref.current.infoLogs[ref.current.infoLogs.length - 1] : null;
    if (last && last.label === label && last.msg && msg) {
      last.msg = `${last.msg} ${msg}`;
      ref.current.infoLogs = [...ref.current.infoLogs];
    } else {
      ref.current.infoLogs = [...ref.current.infoLogs, {label, msg}];
    }

    setInfoLogs(ref.current.infoLogs);
  }, [ref, setInfoLogs]);

  // Write a verbose or detail log, which is very detail for debugging for developer.
  const verbose = React.useCallback((msg) => {
    ref.current.verboseLogs = [...ref.current.verboseLogs, buildLog(msg)];
    setVerboseLogs(ref.current.verboseLogs);
  }, [ref, setVerboseLogs]);

  // Insert some empty lines to info log.
  React.useEffect(() => {
    for (let i = 0; i < 30; i++) {
      info('');
    }
  }, [info]);

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
    <div style={{textAlign: 'right'}}>
      <button onClick={(e) => {
        verbose(`Set debugging to ${!showVerboseLogs}`);
        setShowVerboseLogs(!showVerboseLogs);
      }}>{!showVerboseLogs ? 'Debug' : 'Quit debugging'}</button> &nbsp;
      <button onClick={(e) => {
        verbose(`Set sharing to ${!sharing}`);
        setSharing(!sharing);
      }}>{!sharing ? 'Share' : 'Quit sharing'}</button> &nbsp;
      {showVerboseLogs && <>
        <button onClick={(e) => {
          onClickWelcomeAudio();
        }}>Welcome audio</button> &nbsp;
        <a href="https://github.com/ossrs/ai-talk/discussions" target='_blank' rel='noreferrer'>Help me!</a>
      </>}
    </div>
    <div className={sharing ? 'LogPanelLong' : 'LogPanel'}>
      <div>
        {showVerboseLogs && verboseLogs.map((log, index) => {
          return (<div key={index}>{log}</div>);
        })}
        {!showVerboseLogs && infoLogs.map((log, index) => {
          const you = log.label === 'user';
          const bot = log.label === 'bot';
          const color = you ? 'darkgreen' : (bot ? 'darkblue' : '');
          const fontWeight = you ? 'bold' : 'normal';
          const msg = log.msg ? log.msg : index === infoLogs.length - 1 ? <div><br/><b>Loading...</b></div> : <br/>;
          const prefix = log.msg && log.label ? `${{'sys':'System', 'user':'You', 'bot':'Bot'}[log.label]}: ` : '';
          return (<div key={index} style={{color,fontWeight}}>{prefix}{msg}</div>);
        })}
      </div>
      <div style={{ float:"left", clear: "both" }} ref={logPanelRef}/>
    </div>
  </React.Fragment>];
}
