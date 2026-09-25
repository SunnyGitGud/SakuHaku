-- SakuHaku mpv integration.
--
-- Loaded with --script by SakuHaku (it is embedded in the binary and written
-- to the SakuHaku config dir on each launch, so edit it here, not there).
--
--  * Shows an AniList info card when playback starts and whenever you press
--    "a" (rebind in input.conf with: KEY script-binding sakuhaku/info).
--  * Writes the playback position to a status file every few seconds so
--    SakuHaku can resume later and update your AniList progress.
--
-- Both paths come from the environment:
--   SAKUHAKU_INFO    JSON file with details about the episode (read)
--   SAKUHAKU_STATUS  JSON file with the playback position (written)

local mp = require "mp"
local utils = require "mp.utils"
local assdraw_ok, assdraw = pcall(require, "mp.assdraw")

local info_path = os.getenv("SAKUHAKU_INFO")
local status_path = os.getenv("SAKUHAKU_STATUS")

local info = nil
local overlay = mp.create_osd_overlay("ass-events")
local hide_timer = nil
local announced_watched = false

local function load_info()
    if not info_path or info_path == "" then return end
    local f = io.open(info_path, "r")
    if not f then return end
    local data = f:read("*a")
    f:close()
    info = utils.parse_json(data)
end

-- Escape text for ASS so titles with braces or backslashes render literally.
-- A backslash followed by a zero-width no-break space is never an ASS tag.
local function ass(s)
    s = tostring(s or "")
    s = s:gsub("\\", "\\\239\187\191"):gsub("{", "\\{"):gsub("}", "\\}"):gsub("\n", "\\N")
    return s
end

local function wrap(text, width)
    local lines, line = {}, ""
    for word in tostring(text):gmatch("%S+") do
        if #line + #word + 1 > width and #line > 0 then
            table.insert(lines, line)
            line = word
        else
            line = (#line > 0) and (line .. " " .. word) or word
        end
    end
    if #line > 0 then table.insert(lines, line) end
    return table.concat(lines, "\n")
end

local function hide_info()
    overlay:remove()
    if hide_timer then hide_timer:kill(); hide_timer = nil end
end

local function show_info(duration)
    if not info then load_info() end
    if not info then
        mp.osd_message("SakuHaku: no AniList info for this video", 3)
        return
    end

    local title = "{\\b1\\fs34\\c&HF0A0FF&}" .. ass(info.title) .. "{\\b0\\fs24\\c&HFFFFFF&}"
    local lines = { title }

    local ep = ""
    if info.episode and info.episode > 0 then
        ep = "Episode " .. info.episode
        if info.total_episodes and info.total_episodes > 0 then
            ep = ep .. " / " .. info.total_episodes
        end
    end
    local meta = {}
    for _, v in ipairs({ ep, info.format, info.season, info.score }) do
        if v and v ~= "" then table.insert(meta, v) end
    end
    if #meta > 0 then table.insert(lines, ass(table.concat(meta, "  ·  "))) end

    if info.genres and info.genres ~= "" then table.insert(lines, "{\\c&HBBBBBB&}" .. ass(info.genres) .. "{\\c&HFFFFFF&}") end
    if info.studios and info.studios ~= "" then table.insert(lines, "Studio: " .. ass(info.studios)) end
    if info.next_airing and info.next_airing ~= "" then table.insert(lines, "Next: " .. ass(info.next_airing)) end
    if info.progress and info.progress ~= "" then table.insert(lines, "Your progress: " .. ass(info.progress)) end

    if info.description and info.description ~= "" then
        table.insert(lines, "")
        table.insert(lines, "{\\fs20}" .. ass(wrap(info.description, 80)) .. "{\\fs24}")
    end
    if info.release and info.release ~= "" then
        table.insert(lines, "")
        table.insert(lines, "{\\fs18\\c&H999999&}" .. ass(info.release) .. "{\\c&HFFFFFF&}")
    end

    -- Draw in fixed 1280x720 coordinates, libass scales to the window. (The
    -- real OSD size is often still 0 when the file has just loaded.)
    local text = table.concat(lines, "\\N")
    local _, rows = text:gsub("\\N", "")
    rows = rows + 1
    local box = ""
    if assdraw_ok then
        local a = assdraw.ass_new()
        a:new_event()
        a:append("{\\an7\\pos(0,0)\\bord0\\shad0\\1c&H000000&\\1a&H60&}")
        a:draw_start()
        a:rect_cw(24, 24, 900, math.min(700, 40 + rows * 24))
        a:draw_stop()
        box = a.text .. "\n"
    end

    overlay.res_x = 1280
    overlay.res_y = 720
    overlay.data = box .. "{\\an7\\pos(40,36)\\fs24\\bord1\\shad0}" .. text
    overlay:update()

    if hide_timer then hide_timer:kill() end
    hide_timer = mp.add_timeout(duration or 10, hide_info)
end

local visible = false
mp.add_key_binding("a", "info", function()
    visible = not visible
    if visible then show_info(30) else hide_info() end
end)

-- Status file ---------------------------------------------------------------

-- Remember the last real values: after end-file / during shutdown time-pos
-- is gone and we must not overwrite a good position with 0
local last = { pos = 0, duration = 0, paused = false, eof = false }

local function write_status(eof)
    if not status_path or status_path == "" then return end
    local pos = mp.get_property_number("time-pos")
    local duration = mp.get_property_number("duration")
    if pos then last.pos = pos end
    if duration and duration > 0 then last.duration = duration end
    last.paused = mp.get_property_bool("pause", last.paused)
    if eof then last.eof = true end

    local st = {
        pos = last.pos,
        duration = last.duration,
        paused = last.paused,
        eof = last.eof,
        updated = os.time(),
    }

    local data, err = utils.format_json(st)
    if not data then
        mp.msg.warn("can't encode status: " .. tostring(err))
        return
    end
    local tmp = status_path .. ".tmp"
    local f = io.open(tmp, "w")
    if not f then return end
    f:write(data)
    f:close()
    os.remove(status_path)
    os.rename(tmp, status_path)

    if not announced_watched and info and info.tracking and st.duration > 0 and st.pos / st.duration >= (info.watched_at or 0.85) then
        announced_watched = true
        mp.osd_message("SakuHaku: episode " .. tostring(info.episode) .. " will be marked as watched on AniList", 4)
    end
end

mp.add_periodic_timer(2, function() write_status() end)

mp.register_event("file-loaded", function()
    load_info()
    show_info(8)
end)

mp.register_event("end-file", function(ev)
    write_status(ev.reason == "eof")
end)

mp.register_event("shutdown", function()
    write_status()
end)
