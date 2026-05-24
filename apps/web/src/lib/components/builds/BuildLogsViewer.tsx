import { useEffect, useMemo, useRef, useState, type CSSProperties } from 'react';

import {
	buildLogsStreamUrl,
	getBuildLogArchiveV1,
	listBuildLogsV1,
	type BuildLogArchive,
	type JobLogEntry,
} from '@/lib/api/buildsV1';

const LEVELS = ['TRACE', 'DEBUG', 'INFO', 'WARN', 'ERROR', 'FATAL'] as const;

const LEVEL_STYLE: Record<string, { bg: string; text: string }> = {
	TRACE: { bg: '#1e293b', text: '#94a3b8' },
	DEBUG: { bg: '#1e293b', text: '#60a5fa' },
	INFO: { bg: '#064e3b', text: '#6ee7b7' },
	WARN: { bg: '#78350f', text: '#fcd34d' },
	ERROR: { bg: '#7f1d1d', text: '#fca5a5' },
	FATAL: { bg: '#7f1d1d', text: '#fee2e2' },
};

interface BuildLogsViewerProps {
	buildRid: string;
	isTerminal: boolean;
}

function fmtTime(value: string): string {
	const d = new Date(value);
	return Number.isNaN(d.getTime()) ? value : d.toLocaleTimeString([], { hour: '2-digit', minute: '2-digit', second: '2-digit' });
}

function levelStyle(level: string) {
	return LEVEL_STYLE[level] ?? LEVEL_STYLE.INFO;
}

/**
 * BuildLogsViewer streams the merged log fan-in for a build over SSE
 * while the build is still running, and falls back to a paginated
 * history fetch (plus the archive URI when stamped) once it's
 * terminal. The wire format matches the per-job LiveLogViewer so any
 * stylistic future polish can be shared.
 */
export function BuildLogsViewer({ buildRid, isTerminal }: BuildLogsViewerProps) {
	const [entries, setEntries] = useState<JobLogEntry[]>([]);
	const [paused, setPaused] = useState(false);
	const [activeLevels, setActiveLevels] = useState<Set<string>>(new Set(LEVELS));
	const [search, setSearch] = useState('');
	const [connectionError, setConnectionError] = useState<string | null>(null);
	const [initializingSecondsRemaining, setInitializingSecondsRemaining] = useState<number | null>(null);
	const [archive, setArchive] = useState<BuildLogArchive | null>(null);

	const lastSequenceRef = useRef(0);
	const esRef = useRef<EventSource | null>(null);

	function appendEntry(entry: JobLogEntry) {
		setEntries((prev) => {
			const next = [...prev, entry];
			if (next.length > 5000) return next.slice(-3000);
			return next;
		});
		if (entry.sequence > lastSequenceRef.current) lastSequenceRef.current = entry.sequence;
	}

	// Live stream while the build is hot. Terminal builds switch to
	// history + archive view below.
	useEffect(() => {
		if (isTerminal || paused) return;
		setConnectionError(null);
		const url = buildLogsStreamUrl(buildRid, { from_sequence: lastSequenceRef.current });
		let es: EventSource;
		try {
			es = new EventSource(url, { withCredentials: true });
		} catch (cause) {
			setConnectionError(String(cause));
			return;
		}
		esRef.current = es;
		es.addEventListener('heartbeat', (event) => {
			try {
				const payload = JSON.parse((event as MessageEvent).data) as { phase: string; delay_remaining_seconds: number };
				if (payload.phase === 'initializing') setInitializingSecondsRemaining(payload.delay_remaining_seconds);
			} catch {
				/* ignore */
			}
		});
		es.addEventListener('log', (event) => {
			try {
				const entry = JSON.parse((event as MessageEvent).data) as JobLogEntry;
				setInitializingSecondsRemaining(null);
				appendEntry(entry);
			} catch {
				/* ignore */
			}
		});
		es.onerror = () => {
			setConnectionError('Disconnected — auto-retry on reconnect.');
			es.close();
			esRef.current = null;
		};
		return () => {
			es.close();
			esRef.current = null;
		};
	}, [buildRid, isTerminal, paused]);

	// History fetch on initial mount + on terminal flip. Replaces the
	// in-memory stream contents so the "running → finished" UX has a
	// settled, sorted view.
	useEffect(() => {
		let cancelled = false;
		listBuildLogsV1(buildRid, { limit: 1000 })
			.then((res) => {
				if (cancelled) return;
				setEntries(res.data ?? []);
				if (res.data && res.data.length > 0) {
					lastSequenceRef.current = res.data[res.data.length - 1].sequence;
				}
			})
			.catch((cause) => {
				if (!cancelled) setConnectionError(cause instanceof Error ? cause.message : String(cause));
			});
		return () => {
			cancelled = true;
		};
	}, [buildRid, isTerminal]);

	// Archive lookup. Only meaningful when terminal but cheap enough to
	// run unconditionally — 404 returns null and we render no link.
	useEffect(() => {
		let cancelled = false;
		getBuildLogArchiveV1(buildRid)
			.then((arc) => {
				if (!cancelled) setArchive(arc);
			})
			.catch(() => {
				/* archive endpoint failures are non-fatal */
			});
		return () => {
			cancelled = true;
		};
	}, [buildRid, isTerminal]);

	function toggleLevel(level: string) {
		setActiveLevels((prev) => {
			const next = new Set(prev);
			if (next.has(level)) next.delete(level);
			else next.add(level);
			return next;
		});
	}

	const visible = useMemo(
		() =>
			entries.filter((entry) => {
				if (!activeLevels.has(entry.level)) return false;
				if (search.trim() && !entry.message.toLowerCase().includes(search.trim().toLowerCase())) return false;
				return true;
			}),
		[entries, activeLevels, search],
	);

	return (
		<section className="of-panel" style={{ display: 'grid', gap: 12, padding: 16 }}>
			<header style={{ display: 'flex', alignItems: 'start', justifyContent: 'space-between', gap: 12, flexWrap: 'wrap' }}>
				<div>
					<p className="of-eyebrow" style={{ margin: 0 }}>Build logs</p>
					<h2 className="of-heading-md" style={{ margin: '4px 0 0', fontFamily: 'var(--font-mono)', fontSize: 13 }}>{buildRid}</h2>
					<p className="of-text-muted" style={{ margin: '4px 0 0', fontSize: 12 }}>
						{entries.length} entries · merged across every job
						{!isTerminal && ' · live'}
					</p>
				</div>
				<div style={{ display: 'flex', gap: 8, alignItems: 'center' }}>
					{!isTerminal && (
						<button
							type="button"
							className="of-button"
							style={{ fontSize: 11 }}
							onClick={() => setPaused((p) => !p)}
						>
							{paused ? 'Resume' : 'Pause'}
						</button>
					)}
					{archive?.log_uri && (
						<a
							className="of-button"
							style={{ fontSize: 11, textDecoration: 'none' }}
							href={archive.log_uri}
							target="_blank"
							rel="noreferrer"
						>
							Download archive
						</a>
					)}
					<input
						type="search"
						value={search}
						onChange={(e) => setSearch(e.target.value)}
						placeholder="Search log message"
						className="of-input"
						style={{ fontSize: 11, width: 200 }}
					/>
				</div>
			</header>

			{!isTerminal && initializingSecondsRemaining !== null && initializingSecondsRemaining > 0 && (
				<p style={{ margin: 0, fontSize: 11, color: '#fcd34d' }}>
					Initializing — logs will appear in ~{initializingSecondsRemaining}s
				</p>
			)}
			{connectionError && (
				<div className="of-status-warning" style={{ padding: '8px 10px', borderRadius: 'var(--radius-md)', fontSize: 12 }}>
					{connectionError}
				</div>
			)}

			<div style={{ display: 'flex', gap: 6, flexWrap: 'wrap' }}>
				{LEVELS.map((level) => {
					const active = activeLevels.has(level);
					const style = levelStyle(level);
					return (
						<button
							key={level}
							type="button"
							onClick={() => toggleLevel(level)}
							style={{
								fontSize: 10,
								padding: '2px 8px',
								borderRadius: 999,
								border: 'none',
								background: active ? style.bg : '#1e293b',
								color: active ? style.text : '#94a3b8',
								cursor: 'pointer',
							}}
						>
							{level}
						</button>
					);
				})}
			</div>

			{visible.length === 0 ? (
				<div style={{ padding: 24, border: '1px dashed var(--border-default)', borderRadius: 'var(--radius-md)', textAlign: 'center' }}>
					<p className="of-text-muted" style={{ margin: 0 }}>
						{entries.length === 0
							? isTerminal
								? 'No persisted logs for this build.'
								: 'Waiting for first log line…'
							: 'Filters match no entries.'}
					</p>
				</div>
			) : (
				<div
					style={{
						maxHeight: 520,
						overflow: 'auto',
						border: '1px solid var(--border-default)',
						borderRadius: 'var(--radius-md)',
						background: '#0f172a',
					}}
				>
					<table style={{ width: '100%', borderCollapse: 'collapse', fontSize: 12 }}>
						<thead>
							<tr>
								<th style={headStyle}>Seq</th>
								<th style={headStyle}>Time</th>
								<th style={headStyle}>Level</th>
								<th style={headStyle}>Message</th>
							</tr>
						</thead>
						<tbody>
							{visible.map((entry) => {
								const style = levelStyle(entry.level);
								return (
									<tr key={entry.sequence}>
										<td style={cellStyle}>{entry.sequence}</td>
										<td style={cellStyle}>{fmtTime(entry.ts)}</td>
										<td style={cellStyle}>
											<span style={{ display: 'inline-flex', borderRadius: 999, padding: '1px 7px', background: style.bg, color: style.text, fontWeight: 700 }}>
												{entry.level}
											</span>
										</td>
										<td style={{ ...cellStyle, color: '#f8fafc', whiteSpace: 'pre-wrap' }}>{entry.message}</td>
									</tr>
								);
							})}
						</tbody>
					</table>
				</div>
			)}
		</section>
	);
}

const headStyle: CSSProperties = {
	position: 'sticky',
	top: 0,
	zIndex: 1,
	padding: '7px 8px',
	borderBottom: '1px solid #1f2937',
	background: '#111827',
	color: '#94a3b8',
	fontWeight: 700,
	textAlign: 'left',
};

const cellStyle: CSSProperties = {
	padding: '7px 8px',
	borderBottom: '1px solid #1f2937',
	color: '#cbd5e1',
	fontFamily: 'var(--font-mono)',
	verticalAlign: 'top',
};
