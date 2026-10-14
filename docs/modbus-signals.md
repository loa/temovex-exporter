# REC Modbus signals

The Temovex exporter publishes five temperatures from a REC Corrigo controller. This document is the Modbus map for that controller, and the EXOline-TCP session the exporter uses to read it.

The tables are transcribed from *REC Signals* (AB Regin, Sweden, 2021-11-01). Signal names, addresses, defaults, and descriptions are unchanged, including the original spelling. The TCP section follows `temovex/temovex.go`, `collector.go`, `main.go`, and `playbook.yaml`.

469 signals are listed: 59 input status, 13 coils, 84 input registers, and 313 holding registers. Each scrape reads five of them.

## Modbus map

The controller exposes four Modbus areas. In the tables, a default value marks a setting that SCADA may change. A dash means the list has no default: the signal is an actual value.

The controller address in this list is PLA = 254 and ELA = 30. The display is where that address is changed so it matches the EXOcontroller configuration.

### EXOL types

The type letter is the EXOline storage type.

| Code | Kind | Range |
| --- | --- | --- |
| R | Real | −3.3×10<sup>38</sup> to 3.3×10<sup>38</sup> |
| I | Integer | −32768 to 32767 |
| X | Index | 0 to 255 |
| L | Logic | 0 or 1 |

The Type column is that letter, and sometimes a Modbus area number after a comma. `L,2` is a logic value in Modbus area 2. A letter alone takes its area from the chapter.

### Functions

| Area | Chapter | Read | Write |
| --- | --- | --- | --- |
| 1, coil status | Coil status registers | 1, Read Coils | 5, Write Single Coil; 15, Write Multiple Coils |
| 2, input status | Input status registers | 2, Read Discrete Input | |
| 3, holding register | Holding registers | 3, Read Holding Register | 6, Write Single Register; 16, Write Multiple Registers |
| 4, input register | Input registers | 4, Read Input Register | |

One read covers at most 47 registers.

### Scaling

Modbus carries reals as scaled integers. The register holds the engineering number multiplied by the scale factor. A unit written beside a default (°C, %, s, min, h) is the unit of that number, and it is not sent.

| EXOL type | Scale factor | Register contents |
| --- | ---: | --- |
| Real | 10 | Engineering value × 10 |
| Real time setting | 100 | Engineering value × 100 |
| Integer, index, logic | 1 | The value itself |

`VPac1.Supply_Setp` defaults to 18 °C, so the holding register is 180. `StdObjs1.ConCurve_X1` is an integer defaulting to −20 °C, so the register is −20. Signals described as `HH.MM` or `MM.DD` are the time settings: `TimeDp.Posts(0).T1` defaults to 8, and the register is 800.

### RS485 timing

The list gives these master delays for the serial line:

- At least 3.5 character times between messages, which is 4 ms at 9600 bps.
- At least 14 character times, 16 ms at 9600 bps, between one controller's answer and the request to the next controller when several Corrigos share the RS485 line.

TCP reads from this repository use the EXOline session below.

## EXOline-TCP client

`temovex.NewClient` dials `TEMOVEX_ADDR`. The unit file in `playbook.yaml` sets that to `192.168.3.2:26486`. Port 26486 is the EXOline-TCP port. The dial gives up after one minute. The socket then stays open for the process lifetime, and every scrape reuses it. Later reads and writes have no deadline.

Requests are synchronous. The client writes one frame, reads until `0x3E`, and only then sends the next frame.

`main.go` serves HTTP on `:8080`.

| Route | Behavior |
| --- | --- |
| `/metrics` | Prometheus text from a private registry. The only series is `temovex_temperature_c`. |
| `/health`, `/ready` | Process heartbeat. |
| `/` | Body `temovex-exporter`. |

`temovex_temperature_c` is a gauge with one label, `name`. The collector rounds the float to one decimal place before it is exported.

### Frame

A client frame is:

```
3C | payload | XOR checksum | 3E
```

`0x3C` opens a client message and `0x3E` closes it. `ReadBytes(0x3E)` stops on the closing byte. The checksum is the XOR of the payload only.

A controller frame is:

```
3D | body | XOR checksum | 3E
```

`0x3D` opens the answer. `decode` XORs every byte after `0x3D` and before the checksum, and returns `invalid checksum` when they differ.

### Stuffing

After the checksum is calculated, any payload or checksum byte equal to `0x1B`, `0x3C`, `0x3D`, or `0x3E` is sent as `0x1B` followed by that byte XOR `0xFF`. `unescape` restores the raw bytes before the checksum test, so a stuffed `0x3E` cannot end the frame early.

`TestDecode` covers both cases:

- Wire `3D 05 00 0D 1B E4 C4 41 96 3E` stores a float byte of `0x1B`. `E4 XOR FF` is `1B`, and the float bytes are `0D 1B C4 41` (24.513208 °C).
- Wire `3D 05 00 2B E9 BA 41 1B C3 3E` has checksum `0x3C`, stuffed as `1B C3`. The float bytes are `2B E9 BA 41` (23.363852 °C).

An unstuffed sample in the same test, `3D 05 00 00 00 98 41 DC 3E`, is 19.0 °C. The checksum `DC` is `05 XOR 00 XOR 00 XOR 00 XOR 98 XOR 41`.

### Read request

Each temperature method sends eight payload bytes. The first five are fixed:

| Offset | Byte | Meaning |
| ---: | --- | --- |
| 0 | `FF` | PLA used on a direct EXOline-TCP session |
| 1 | `1E` | ELA 30, the default controller address in the signal list |
| 2 | `C8` | Multi-command wrapper around the following command |
| 3 | `04` | Length of that command, in bytes |
| 4 | `B6` | Read a real parameter. The answer is an IEEE-754 binary32 |

The last three bytes select the variable: line number, then two address bytes. The cell index those bytes name is `high × 60 + low`. `GetSet`, `GetUL`, `GetTL`, `GetFL`, and `GetAL` each pass a fixed array to `getFloat`.

Byte 0 is `FF`, the PLA this direct TCP session sends. The controller PLA in the signal list is 254. Byte 1 is ELA 30 (`1E`), the same ELA as the list.

Extract air (`GetFL`) on the wire:

```
payload   FF 1E C8 04 B6 04 08 00
checksum  FF xor 1E xor C8 xor 04 xor B6 xor 04 xor 08 xor 00 = 97
frame     3C FF 1E C8 04 B6 04 08 00 97 3E
```

### Read response

For these reads the decoded body is a two-byte header and a little-endian float32. Every answer fixture in `temovex/temovex_test.go` uses the header `05 00`:

```
3D | 05 | 00 | float32 little-endian | checksum | 3E
```

`getFloat` parses `msg[3 : len(msg)-2]`, which is those four float bytes.

### Values scraped

The method names use the Temovex abbreviations: UL is outdoor air (uteluft), TL is supply air (tilluft), FL is extract air (frånluft), and AL is exhaust air (avluft). `GetSet` is the supply-air setpoint. The collector asks for them in the order setpoint, exhaust, extract, outdoor, supply. A read error stops the process with `log.Fatal`.

The low address byte is greater than 59 for outdoor, supply, and exhaust. Those three frames are stored that way in the client. The cell column is still `high × 60 + low`.

| Method | `name` label | Reading | Line | Address bytes | Cell | Checksum | Payload |
| --- | --- | --- | ---: | --- | ---: | --- | --- |
| `GetSet` | `set` | Supply-air setpoint | 4 | `00 08` | 8 | `97` | `FF 1E C8 04 B6 04 00 08` |
| `GetAL` | `al` | Exhaust air | 59 (`3B`) | `00 95` | 149 | `35` | `FF 1E C8 04 B6 3B 00 95` |
| `GetFL` | `fl` | Extract air | 4 | `08 00` | 480 | `97` | `FF 1E C8 04 B6 04 08 00` |
| `GetUL` | `ul` | Outdoor air | 4 | `04 EC` | 476 | `77` | `FF 1E C8 04 B6 04 04 EC` |
| `GetTL` | `tl` | Supply air | 57 (`39`) | `02 FA` | 370 | `5A` | `FF 1E C8 04 B6 39 02 FA` |

## Input status registers

Area 2, function 2. 59 discrete inputs, addresses 1–89. Addresses 9–12 and 25–50 are not assigned.

| Addresses | Signals |
| --- | --- |
| 1–8 | Digital inputs DI1–DI8 |
| 13–19 | Digital output feedback DQ1–DQ7 |
| 20–24 | Supply fan, exhaust fan, defrost, calibration, demand control |
| 51–89 | Alarm points `AlaPts.*` |

| Address | Signal | Type | Default | Description |
| ---: | --- | --- | --- | --- |
| 1 | `QDig.DI1` | L,2 | — | Value of DI1<br>(UnitOnOff) |
| 2 | `QDig.DI2` | L,2 | — | Value of DI2<br>(External Input) |
| 3 | `QDig.DI3` | L,2 | — | Value of DI3<br>(External Input) |
| 4 | `QDig.DI4` | L,2 | — | Value of DI4<br>(Alarm Supply Fan) |
| 5 | `QDig.DI5` | L,2 | — | Value of DI5<br>(Alarm Exhaust Fan) |
| 6 | `QDig.DI6` | L,2 | — | Value of DI6<br>(External Input) |
| 7 | `QDig.DI7` | L,2 | — | Value of DI7<br>(Alarm Electric Heater) |
| 8 | `QDig.DI8` | L,2 | — | Value of DI8<br>(AlarmFilter) |
| 13 | `QDig.Dq1` | L,2 | — | Value of DO1<br>(Bypass Open) |
| 14 | `QDig.Dq2` | L,2 | — | Value of DO2<br>(Bypass Close) |
| 15 | `QDig.Dq3` | L,2 | — | Value of DO3<br>(PWM) |
| 16 | `QDig.Dq4` | L,2 | — | Value of DO4<br>(Not active,KAVK, ULS, Summalarm, CP Heating, CP Cooling,Normal flow) |
| 17 | `QDig.Dq5` | L,2 | — | Value of DO5<br>(Not active,KAVK, ULS, Summalarm, CP Heating, CP Cooling,Normal flow) |
| 18 | `QDig.Dq6` | L,2 | — | Value of DO6<br>(Not active,KAVK, ULS, Summalarm, CP Heating, CP Cooling,Normal flow) |
| 19 | `QDig.Dq7` | L,2 | — | Value of DO7<br>(Not active,KAVK, ULS, Summalarm, CP Heating, CP Cooling,Normal flow) |
| 20 | `VPac1.SAF_Start` | L | — | Run indication SAF<br>0 = Not running<br>1 = Running |
| 21 | `VPac1.EAF_Start` | L | — | Run indication EAF<br>0 = Not running<br>1 = Running |
| 22 | `VPac1.Defrosting` | L | — | Unit in defrosting mode<br>0 = Normal run<br>1 = Defrosting |
| 23 | `VPac1.CalibrationRunning` | L | — | Unit in calibration mode<br>0 = Normal run<br>1 = Calibration running |
| 24 | `VPac1.DemandCtrl_Active` | L | — | Demand controlled supply regulation active output (output from pids >0%) |
| 51 | `AlaPts.OutDoorTemp_SensorAlarm` | L,2 | — | Sensor error outdoor temp.<br>0=No alarm<br>1=Alarm |
| 52 | `AlaPts.SupplyAirTemp_SensorAlarm` | L,2 | — | Sensor error Supply Air temp. |
| 53 | `AlaPts.ExhaustAirTemp_SensorAlarm` | L,2 | — | Sensor error Exhaust Air temp. |
| 54 | `AlaPts.ExtractAirTemp_SensorAlarm` | L,2 | — | Sensor error Extract Air temp |
| 55 | `AlaPts.RoomTemp_SensorAlarm` | L,2 | — | Sensor error Room temp. |
| 56 | `AlaPts.FrostProtTemp_SensorAlarm` | L,2 | — | Sensor error Frost Protection temp. |
| 57 | `AlaPts.FrostProt_Alarm` | L,2 | — | Frost risk |
| 58 | `AlaPts.SAF_Alarm` | L,2 | — | Run Error Supply Air Fan |
| 59 | `AlaPts.EAF_Alarm` | L,2 | — | Run Error Extract Air Fan |
| 60 | `AlaPts.PreH_Damper_Manual_Alarm` | L,2 | — | Manual Preheat damper control |
| 61 | `AlaPts.ElectricHeater_Alarm` | L,2 | — | Electric heating is overheated |
| 62 | `AlaPts.Filter_Alarm` | L,2 | — | Filter Guard |
| 63 | `AlaPts.CoolingManual_Alarm` | L,2 | — | Manual Cooler control |
| 64 | `AlaPts.HeatingManual_Alarm` | L,2 | — | Manual Heater control |
| 65 | `AlaPts.ByPassManual_Alarm` | L,2 | — | Manual Bypass control |
| 66 | `AlaPts.ULSManual_Alarm` | L,2 | — | Manual ULS control |
| 67 | `AlaPts.KAVKManual_Alarm` | L,2 | — | Manual KAVK control |
| 68 | `AlaPts.P1HeatingManual_Alarm` | L,2 | — | Manual CP-Heating |
| 69 | `AlaPts.P1CoolingManual_Alarm` | L,2 | — | Manual CP-Cooling |
| 70 | `AlaPts.SAFManual_Alarm` | L,2 | — | Manual Supply Air Fan mode |
| 71 | `AlaPts.EAFManual_Alarm` | L,2 | — | Manual Extract Air Fan mode |
| 72 | `AlaPts.SupplyManual_Alarm` | L,2 | — | Manual supply air control |
| 73 | `AlaPts.InternalBatteryError` | L,2 | — | Internal battery error |
| 74 | `AlaPts.FilterAlarmTime` | L,2 | — | Filter Alarm |
| 75 | `AlaPts.FireDamperAlarm` | L,2 | — | Fire damper alarm |
| 76 | `AlaPts.FireAlarm` | L,2 | — | Fire alarm |
| 77 | `AlaPts.TGR4_SensorAlarm` | L,2 | — | Sensor error TG-R4 |
| 78 | `AlaPts.FF_Block` | L,2 | — | Warning temperature deviation |
| 79 | `AlaPts.PreH_Elec_Manual_Alarm` | L,2 | — | Preheat electric manual |
| 80 | `AlaPts.Supply_low_alarm` | L,2 | — | Supply air temp low limit |
| 81 | `AlaPts.Ductheater_Manual_Alarm` | L,2 | — | Duct heater manual |
| 82 | `AlaPts.Ductheatertemp_SensorAlarm` | L,2 | — | Sensor error duct heater temp |
| 83 | `AlaPts.Afterexchanger_SensorAlarm` | L,2 | — | Sensor Error After Exchanger Air Temp. |
| 84 | `AlaPts.DefrostFailA_Alarm` | L,2 | — | Defrost Fail A-Alarm |
| 85 | `AlaPts.DefrostFailB_Alarm` | L,2 | — | Defrost Fail B-Alarm |
| 86 | `AlaPts.Hatch_Manual_Alarm` | L,2 | — | Hatch manual |
| 87 | `AlaPts.Autocalibration_Alarm` | L,2 | — | Autocalibration not finished |
| 88 | `AlaPts.FireIndication_Alarm` | L,2 | — | Fire indication |
| 89 | `AlaPts.SmokeDetectorError_Alarm` | L,2 | — | Smoke detector error |

## Coil status registers

Area 1. Read with function 1. Write with function 5 or 15. All 13 addresses from 1 to 13 are assigned, and each has a default.

`VPac1.HeatType` at coil 1 is marked as moved. The index value used now is holding register 124.

| Address | Signal | Type | Default | Description |
| ---: | --- | --- | --- | --- |
| 1 | `VPac1.HeatType` | L | 1 | Changed datatype, now placed in Holding Register 124. |
| 2 | `VPac1.Activate_CoolRecycle` | L | 1 | Activates Cool recycling<br>0 = Not Active<br>1 = Active |
| 3 | `VPac1.NightCool_Enable` | L | 0 | Activates Night cooling<br>0 = Not Active<br>1 = Active |
| 4 | `VPac1.UseTimeGr` | L | 0 | Activates timer<br>0 = Not Active<br>1 = Active |
| 5 | `VPac1.FilterAlarm_Activate` | L | 1 | Activates the filter alarm<br>0 = Not Active<br>1 = Active |
| 6 | `VPac1.FilterAlarm_ResetTime` | L | 0 | Resets the filter alarm time<br>1 = resetting the time |
| 7 | `VPac1.RUDispAway` | L | 0 | Room unit away<br>0 = Home<br>1 = Away |
| 8 | `VPac1.StopDefrost` | L | 1 | Stop defrost option. |
| 9 | `VPac1.HeatValveExercise` | L | 0 | Enable water heat valve (0-10V) exercise |
| 10 | `VPac1.DemandCtrlTemp_Enable` | L | 0 | Demand controlled supply regulation with temperature (0=Disabled, 1=Enabled) |
| 11 | `VPac1.DemandCtrlCO2_Enable` | L | 0 | Demand controlled supply regulation with CO2 (0=Disabled, 1=Enabled) |
| 12 | `VPac1.DemandCtrlHumitidy_Enable` | L | 0 | Demand controlled supply regulation with humidity (0=Disabled, 1=Enabled) |
| 13 | `VPac1.DemandCtrlTemp_Control` | L | 1 | Demand controlled supply regulation temperature control mode (0=Heating,1=Cooling) |

## Input registers

Area 4, function 4. 84 registers, addresses 1–89. Addresses 46–50 are not assigned. None of these rows has a default.

| Addresses | Signals |
| --- | --- |
| 1–8 | Analog inputs `QAnaIn.AI1`–`AI8` |
| 9–13 | Analog outputs `QanaOut.AQ1`–`AQ5` |
| 14–17 | Scaled input/output values |
| 18–45 | Temperatures, exchanger status, and demand-control outputs (`VPac1`, `StdObjs1`, `RoomUnit`) |
| 51–89 | Alarm status `AlaData.*`, using the same address numbers as `AlaPts.*` in the input-status area |

| Address | Signal | Type | Default | Description |
| ---: | --- | --- | --- | --- |
| 1 | `QAnaIn.AI1` | R | — | The raw value of AI1<br>(Outdoor temperature) |
| 2 | `QAnaIn.AI2` | R | — | The raw value of AI2<br>(Supply air temperature) |
| 3 | `QAnaIn.AI3` | R | — | The raw value of AI3<br>(Exhaust air temperature) |
| 4 | `QAnaIn.AI4` | R | — | The raw value of AI4<br>(Extract air temperature) |
| 5 | `QAnaIn.AI5` | R | — | The raw value of AI5<br>(Room temperature) |
| 6 | `QAnaIn.AI6` | R | — | The raw value of AI6<br>(Frost protection temperature) |
| 7 | `QAnaIn.AI7` | R | — | The raw value of AI7<br>(TGR4) |
| 8 | `QAnaIn.AI8` | R | — | The raw value of AI8<br>(Preheat temperature) |
| 9 | `QanaOut.AQ1` | R | — | Value of AO1<br>(SAF Speed) |
| 10 | `QanaOut.AQ2` | R | — | Value of AO2<br>(EAF Speed) |
| 11 | `QanaOut.AQ3` | R | — | Value of AO3<br>(Heating valve) |
| 12 | `QanaOut.AQ4` | R | — | Value of AO4<br>(Cooling valve) |
| 13 | `QanaOut.AQ5` | R | — | Value of AO5<br>(Preheat valve) |
| 14 | `InputOutput.OutDoorTemp` | R | — | Outdoor temperature:<br>The scaled and filtered value of AI1 |
| 15 | `InputOutput.SupplyAirTemp` | R | — | Supply air temperature:<br>The scaled and filtered value of AI2 |
| 16 | `InputOutput.ExhaustAirTemp` | R | — | Exhaust air temperature:<br>The scaled and filtered value of AI3 |
| 17 | `InputOutput.ExtractAirTemp` | R | — | Extract air temperature:<br>The scaled and filtered value of AI4 |
| 18 | `VPac1.RoomTemp` | R | — | Room temperature<br>The scaled and filtered value of Roomtemp |
| 19 | `VPac1.FrostProtTemp` | R | — | Frost protection temperature<br>The scaled and filtered value of Frostprottemp |
| 20 | `VPac1.FilterAlarm_TimeLeft` | I | — | Time left before the filter alarm activates |
| 21 | `VPac1.Disp_SplitRange_Output2` | R | — | Bypass output |
| 22 | `VPac1.Preheat_fan_offset` | R | — | The offset added to SAF speed to compensate for open preheat damper |
| 23 | `StdObjs1.ConCurve_Output` | R | — | Outdoor compensated setpoint (Is always calculated, even when not used). |
| 24 | `StdObjs1.PWMCtrl_Input` | R | — | Electric heater output (if selected) |
| 25 | `VPac1.DesiredTemp` | R | — | Setpoint (after  compensation from i.e. TG-R4) |
| 26 | `VPac1.DuctHeaterTemp` | R | — | Duct heater temperature<br>The scaled and filtered value of Duct heater |
| 27 | `VPac1.Aux2` | R | — | Option temperature<br>The scaled and filtered value of Option temp |
| 28 | `VPac1.LivingroomTemp` | R | — | Livingroom temperature (Room unit 2) |
| 29 | `StdObjs1.PWMhand_Ductheater_Output` | R | — | Duct heater output (if selected) |
| 30 | `RoomUnit.RU1Temperature` | R | — | Room unit 1 temperature |
| 31 | `VPac1.TempAfterExchanger` | R | — | Temperature after the exchanger (before heater) |
| 32 | `VPac1.SAFPressure` | R | — | Supply air fan pressure (if configured) |
| 33 | `VPac1.EAFPressure` | R | — | Exhaust air fan pressure (if configured) |
| 34 | `VPac1.ExchangerPressureDrop` | R | — | Pressure drop over the exchanger (if configured) |
| 35 | `VPac1.FrostProtStatus` | X | — | Describe the antifreeze status:<br>0=Off<br>1=Normal pressure reduced setpoint<br>2=Normal pressure cycle pause<br>3=Reduced pressure cycle normal<br>4=Reduced pressure cycle pause<br>5=Stop |
| 36 | `VPac1.CurrentPressureSetP` | R | — | The actual exchanger pressuredrop setpoint for the current fan speed (Pa) |
| 37 | `VPac1.RunningFreezeCycle` | X | — | On current freeze cycle, 0=Off, >0 running cycle number |
| 38 | `VPac1.CO2Sensor` | R | — | CO2 Sensor |
| 39 | `VPac1.HumiditySensor` | R | — | Humidity sensor |
| 40 | `VPac1.DemandCtrlTempPID_Output` | R | — | Demand controlled supply regulation temperature PID output |
| 41 | `VPac1.DemandCtrlCO2PID_Output` | R | — | Demand controlled supply regulation CO2 PID output |
| 42 | `VPac1.DemandCtrlHumPID_Output` | R | — | Demand controlled supply regulation humidity PID output |
| 43 | `VPac1.DemandCtrlSumPID_MaxOutput` | R | — | Demand controlled supply regulation sum PID max output (%) |
| 44 | `VPac1.DemandCtrlSum_SAFOutput` | R | — | Demand controlled supply regulation sum output in fanspeed SAF (%) |
| 45 | `VPac1.DemandCtrlSum_EAFOutput` | R | — | Demand controlled supply regulation sum output in fanspeed EAF (%) |
| 51 | `AlaData.OutDoorTemp_SensorAlarm_Status` | X | — | Sensor error outdoor temp. (Alarm no.1)<br>0=Not used<br>1=Normal<br>2=Blocked<br>3=Acknowledge<br>4=Not used<br>5=Cancelled<br>6=Not used<br>7=Alarm |
| 52 | `AlaData.SupplyAirTemp_SensorAlarm_Status` | X | — | Sensor error Supply Air temp. (Alarm no.2) |
| 53 | `AlaData.ExhaustAirTemp_SensorAlarm_Status` | X | — | Sensor error Exhaust Air temp. (Alarm no.3) |
| 54 | `AlaData. ExtractAirTemp_SensorAlarm_Status` | X | — | Sensor error Extract Air temp (Alarm no.4) |
| 55 | `AlaData.RoomTemp_SensorAlarm_Status` | X | — | Sensor error Room temp. (Alarm no.5) |
| 56 | `AlaData.FrostProtTemp_SensorAlarm_Status` | X | — | Sensor error Frost Prot. temp. (Alarm no.6) |
| 57 | `AlaData.FrostProt_Alarm_Status` | X | — | Frost risk (Alarm no.7) |
| 58 | `AlaData.SAF_Alarm_Status` | X | — | Run Error Supply Air Fan (Alarm no.8) |
| 59 | `AlaData.EAF_Alarm_Status` | X | — | Run Error Extract Air Fan (Alarm no.9) |
| 60 | `AlaData.PreH_Damper_Manual_Alarm_Status` | X | — | Preheat damper manual (Alarm no.10) |
| 61 | `AlaData.ElectricHeater_Alarm_Status` | X | — | Electric heating is overheated (Alarm no.11) |
| 62 | `AlaData.Filter_Alarm_Status` | X | — | Filter Guard (Alarm no.12) |
| 63 | `AlaData.CoolingManual_Alarm_Status` | X | — | Manual Cooler control (Alarm no.13) |
| 64 | `AlaData.HeatingManual_Alarm_Status` | X | — | Manual Heater control (Alarm no.14) |
| 65 | `AlaData.ByPassManual_Alarm_Status` | X | — | Manual Bypass control (Alarm no.15) |
| 66 | `AlaData.ULSManual_Alarm_Status` | X | — | Manual ULS control (Alarm no.16) |
| 67 | `AlaData.KAVKManual_Alarm_Status` | X | — | Manual KAVK control (Alarm no.17) |
| 68 | `AlaData.P1HeatingManual_Alarm_Status` | X | — | Manual CP-Heating (Alarm no.18) |
| 69 | `AlaData.P1CoolingManual_Alarm_Status` | X | — | Manual CP-Cooling (Alarm no.19) |
| 70 | `AlaData.SAFManual_Alarm_Status` | X | — | Manual Supply Air Fan mode (Alarm no.20) |
| 71 | `AlaData.EAFManual_Alarm_Status` | X | — | Manual Extract Air Fan mode (Alarm no.21) |
| 72 | `AlaData.SupplyManual_Alarm_Status` | X | — | Manual supply air control (Alarm no.22) |
| 73 | `AlaData.InternalBatteryError_Status` | X | — | Internal battery error (Alarm no.23) |
| 74 | `AlaData.FilterAlarmTime_Status` | X | — | Filter alarm (Alarm no.24) |
| 75 | `AlaData.FireDamperAlarm_Status` | X | — | Fire damper alarm (Alarm no.25) |
| 76 | `AlaData.FireAlarm_Status` | X | — | Fire alarm (Alarm no.26) |
| 77 | `AlaData.TGR4_SensorAlarm_Status` | X | — | Sensor error TG-R4 (Alarm no.27) |
| 78 | `AlaData.FF_Block_Status` | X | — | Warning temperature deviation (Alarm no.28) |
| 79 | `AlaData.PreH_Elec_Manual_Alarm_Status` | X | — | Preheat electric manual (Alarm no.29) |
| 80 | `AlaData.Supply_low_alarm_Status` | X | — | Supply air temp low limit (Alarm no.30) |
| 81 | `AlaData.Ductheater_Manual_Alarm_Status` | X | — | Duct heater manual (Alarm no.31) |
| 82 | `AlaData.Ductheatertemp_SensorAlarm_Status` | X | — | Sensor error duct heater temp (Alarm no.32) |
| 83 | `AlaData.Afterexchanger_SensorAlarm_Status` | X | — | Sensor error After Exch. Temp (Alarm no.33) |
| 84 | `AlaData.DefrostFailA_Alarm_Status` | X | — | Defrost Fail A-Alarm (Alarm no.34) |
| 85 | `AlaData.DefrostFailB_Alarm_Status` | X | — | Defrost Fail B-Alarm (Alarm no.35) |
| 86 | `AlaData.Hatch_Manual_Alarm_Status` | X | — | Hatch manual (Alarm no.36) |
| 87 | `AlaData.Autocalibration_Alarm_Status` | X | — | Autocalibration not finished (Alarm no.37) |
| 88 | `AlaData.FireIndication_Alarm_Status` | X | — | Fire indication (Alarm no.38) |
| 89 | `AlaData.SmokeDetectorError_Alarm_Status` | X | — | Smoke detector error (Alarm no.39) |

## Holding registers

Area 3. Read with function 3. Write with function 6 or 16. 313 registers, addresses 1–437. Addresses with no row below are not assigned in this list.

`VPac1` is interleaved with the other prefixes. `InputOutput` is 65–68, `TimeDp` is 201–232, `TimeHp` is 301–348, `Alarms` is 401–403, and `QSystem` is 411–418. `StdObjs1` is spread from 8 through 137. `TimeDp` and `TimeHp` are the time settings scaled by 100.

| Address | Signal | Type | Default | Description |
| ---: | --- | --- | --- | --- |
| 1 | `VPac1.Regulation` | X | 0 | Supply air control, Outdoor comp. supply, Room control, Exhaust air control |
| 2 | `VPac1.Supply_Setp` | R | 18 °C | Setpoint supply air temperature<br>(If supply air control function) |
| 3 | `VPac1.ExhaustSetP` | R | 21 °C | Setpoint Exhaust air temp<br>(if extract air temp control function) |
| 4 | `VPac1.RoomSetP` | R | 21 °C | Room setpoint<br>(if room temp control function) |
| 5 | `VPac1.SetPAdj` | R | 2 °C | Adjustment of setpoint in °C when away |
| 6 | `VPac1.SupplySetpointMax` | R | 30 °C | Max limit of supply setpoint when cascade control |
| 7 | `VPac1.SupplySetpointMin` | R | 17 °C | Min limit of supply setpoint when cascade control |
| 8 | `StdObjs1.ConCurve_X1` | I | -20 °C | Outdoortemp for first curvepoint for outdoor compensated setpoint |
| 9 | `StdObjs1.ConCurve_X2` | I | -15 °C | Outdoortemp for second curvepoint for outdoor compensated setpoint |
| 10 | `StdObjs1.ConCurve_X3` | I | -10 °C | Outdoortemp for third curvepoint for outdoor compensated setpoint |
| 11 | `StdObjs1.ConCurve_X4` | I | -5 °C | Outdoortemp for fourth curvepoint for outdoor compensated setpoint |
| 12 | `StdObjs1.ConCurve_X5` | I | 0 °C | Outdoortemp for fifth curvepoint for outdoor compensated setpoint |
| 13 | `StdObjs1.ConCurve_X6` | I | 5 °C | Outdoortemp for sixth curvepoint for outdoor compensated setpoint |
| 14 | `StdObjs1.ConCurve_X7` | I | 10 °C | Outdoortemp for seventh curvepoint for outdoor compensated setpoint |
| 15 | `StdObjs1.ConCurve_X8` | I | 15 °C | Outdoortemp for eighth curvepoint for outdoor compensated setpoint |
| 16 | `StdObjs1.ConCurve_Y1` | I | 25 °C | Setpoint for first curvepoint for outdoor compensated setpoint |
| 17 | `StdObjs1.ConCurve_Y2` | I | 24 °C | Setpoint for second curvepoint for outdoor compensated setpoint |
| 18 | `StdObjs1.ConCurve_Y3` | I | 23 °C | Setpoint for third curvepoint for outdoor compensated setpoint |
| 19 | `StdObjs1.ConCurve_Y4` | I | 23 °C | Setpoint for fourth curvepoint for outdoor compensated setpoint |
| 20 | `StdObjs1.ConCurve_Y5` | I | 22 °C | Setpoint for fifth curvepoint for outdoor compensated setpoint |
| 21 | `StdObjs1.ConCurve_Y6` | I | 20 °C | Setpoint for sixth curvepoint for outdoor compensated setpoint |
| 22 | `StdObjs1.ConCurve_Y7` | I | 18 °C | Setpoint for seventh curvepoint for outdoor compensated setpoint |
| 23 | `StdObjs1.ConCurve_Y8` | I | 18 °C | Setpoint for eighth curvepoint for outdoor compensated setpoint |
| 24 | `VPac1.SAF_Min` | R | 20 % | Min flow Supply Air Fan |
| 25 | `VPac1.EAF_Min` | R | 25 % | Min flow Exhaust Air Fan |
| 26 | `VPac1.SAF_Normal` | R | 50 % | Normal flow Supply Air Fan |
| 27 | `VPac1.EAF_Normal` | R | 55 % | Normal flow Exhaust Air Fan |
| 28 | `VPac1.SAF_FF` | R | 80 % | Forced flow Supply Air Fan |
| 29 | `VPac1.EAF_FF` | R | 85 % | Forced flow Exhaust Air Fan |
| 30 | `VPac1.SAF_Max` | R | 100 % | Max flow Supply Air Fan |
| 31 | `VPac1.EAF_Max` | R | 100 % | Max flow Exhaust Air Fan |
| 32 | `VPac1.SAF_Hood` | R | 80 % | Hood flow Supply Air Fan |
| 33 | `VPac1.EAF_Hood` | R | 20 % | Hood flow Exhaust Air Fan |
| 34 | `VPac1.SAF_Fire` | R | 80 % | Fire flow Supply Air Fan |
| 35 | `VPac1.EAF_Fire` | R | 20 % | Fire flow Exhaust Air Fan |
| 36 | `VPac1.SAF_FireAlarm` | R | 0 % | Fire Alarm flow Supply Air Fan |
| 37 | `VPac1.EAF_FireAlarm` | R | 100 % | Fire Alarm flow Exhaust Air Fan |
| 38 | `VPac1.SAF_NightCool` | R | 50 % | Night Cooling flow Supply Air Fan |
| 39 | `VPac1.EAF_NightCool` | R | 55 % | Night Cooling flow Exhaust Air Fan |
| 40 | `VPac1.SAF_StartDelay` | I | 0 s | Start delay Supply Air Fan |
| 41 | `VPac1.SAF_StopDelay` | I | 60 s | Stop delay Supply Air Fan |
| 42 | `VPac1.EAF_StartDelay` | I | 0 s | Start delay Exhaust Air Fan |
| 43 | `VPac1.EAF_StopDelay` | I | 0 s | Stop delay Exhaust Air Fan |
| 44 | `VPac1.Fan_Mode` | X | 1 | Fan control:<br>0=Off<br>1=Auto<br>2=Min flow<br>3=Boost flow<br>4=Max flow<br>5=Stove flow<br>6=Kitchen flow<br>7=ECO<br>8=Fire flow |
| 45 | `StdObjs1.PID_Supply_Select` | X | 2 | Supply temp controller mode:<br>0=Manual off<br>1=Manual on<br>2=Auto |
| 46 | `VPac1.SAF_Mode` | X | 2 | Running mode SAF:<br>0=Off<br>1=Manual<br>2=Auto |
| 47 | `VPac1.EAF_Mode` | X | 2 | Running mode EAF:<br>0=Off<br>1=Manual<br>2=Auto |
| 48 | `VPac1.Heating_Mode` | X | 2 | Running mode Heating:<br>0=Off<br>1=Manual<br>2=Auto |
| 49 | `VPac1.ByPass_Mode` | X | 2 | Running mode Bypass:<br>0=Off<br>1=Manual<br>2=Auto |
| 50 | `VPac1.Cooling_Mode` | X | 2 | Running mode Cooling:<br>0=Off<br>1=Manual<br>2=Auto |
| 51 | `VPac1.ULS_Mode` | X | 2 | Running mode ULS:<br>0=Manual closed<br>1=Manual opened<br>2=Auto |
| 52 | `VPac1.KAVK_Mode` | X | 2 | Running mode KAVK:<br>0= Manual off<br>1=Manual on<br>2=Auto |
| 53 | `VPac1.P1Heating_Mode` | X | 2 | Running mode CP-Heating:<br>0=Manual off<br>1=Manual on<br>2=Auto |
| 54 | `VPac1.P1Cooling_Mode` | X | 2 | Running mode CP-Cooling:<br>0=Manual off<br>1=Manual on<br>2=Auto |
| 55 | `StdObjs1.PID_Supply_ManSet` | R | 0 % | Supply temp controller output if manual mode |
| 56 | `VPac1.SAF_Manual` | R | 0 % | Supply Air Fan output if manual mode |
| 57 | `VPac1.EAF_Manual` | R | 0 % | Exhaust Air Fan output if manual mode |
| 58 | `VPac1.Heating_Manual` | R | 0 % | Heating controller output if manual mode |
| 59 | `VPac1.ByPass_Manual` | R | 0 % | Bypass output if manual mode |
| 60 | `VPac1.Cooling_Manual` | R | 0 % | Cooling controller output if manual mode |
| 61 | `VPac1.PGain_Supply_PID` | R | 33 °C | P-band supply air control |
| 62 | `VPac1.PGain_Cascade_PID` | R | 100 °C | P-band cascade control |
| 63 | `VPac1.ITime_Supply_PID` | R | 100 s | I-time supply air control |
| 64 | `VPac1.ITime_Cascade_PID` | R | 300 s | I-time cascade control |
| 65 | `InputOutput.OutDoorTempOffset` | R | 0 °C | Offset to calibrate Ai1:<br>(Outdoor temperature) |
| 66 | `InputOutput.SupplyAirTempOffset` | R | 0 °C | Offset to calibrate Ai2:<br>(Supply air temperature) |
| 67 | `InputOutput.ExhaustAirTempOffset` | R | 0 °C | Offset to calibrate Ai3:<br>(Exhaust air temperature) |
| 68 | `InputOutput.ExtractAirTempOffset` | R | 0 °C | Offset to calibrate Ai4:<br>(Extract air temperature) |
| 69 | `VPac1.RoomTempOffset` | R | 0 °C | Offset to calibrate Roomtemp:<br>(Room temperature) |
| 70 | `VPac1.FrostProtTempOffset` | R | 0 °C | Offset to calibrate Frosttemp:<br>(Frost protection temperature) |
| 71 | `VPac1.DICtrl1` | X | — | Function on DI1:<br>0 = Not activated<br>1 = Min flow<br>2 = Boost flow<br>3 = Max flow<br>4 = Kitchen mode<br>5 = ECO<br>6 = Stove mode<br>7 = Fire Input<br>8 = Fire damper<br>9 = AHU On/Off<br>10 = Normal<br>11 = Filterguard fire<br>12 = Smoke det. error |
| 72 | `VPac1.DICtrl2` | X | 3 | Function on DI2:<br>0 = Not activated<br>1 = Min flow<br>2 = Boost flow<br>3 = Max flow<br>4 = Kitchen mode<br>5 = ECO<br>6 = Stove mode<br>7 = Fire Input<br>8 = Fire damper<br>9 = AHU On/Off<br>10 = Normal<br>11 = Filterguard fire<br>12 = Smoke det. error |
| 73 | `VPac1.DICtrl3` | X | 3 | Function on DI3:<br>0 = Not activated<br>1 = Min flow<br>2 = Boost flow<br>3 = Max flow<br>4 = Kitchen mode<br>5 = ECO<br>6 = Stove mode<br>7 = Fire Input<br>8 = Fire damper<br>9 = AHU On/Off<br>10 = Normal<br>11 = Filterguard fire<br>12 = Smoke det. error |
| 74 | `VPac1.DICtrl4` | X | 0 | Function on DI4 (Supply Air Fan):<br>0=Tacho<br>1=Frequency alarm |
| 75 | `VPac1.DICtrl5` | X | 0 | Function on DI5 (Exhaust Air):<br>0=Tacho<br>1=Frequency alarm |
| 76 | `VPac1.DICtrl6` | X | — | Function on DI6:<br>0 = Not activated<br>1 = Min flow<br>2 = Boost flow<br>3 = Max flow<br>4 = Kitchen mode<br>5 = ECO<br>6 = Stove mode<br>7 = Fire Input<br>8 = Fire damper<br>9 = AHU On/Off<br>10 = Normal<br>11 = Filterguard fire<br>12 = Smoke det. error |
| 77 | `VPac1.DO6Ctrl` | X | 1 | Function on DO6:<br>0 = Not activated<br>1 = KAVK<br>2 = ULS<br>3 = Summalarm<br>4 = CP Heating<br>5 = CP Cooling<br>6 = Normal flow<br>7 = Fire damper<br>8 = Not selectable<br>9 = Stove<br>10 = Section hatch (for section exchanger units)<br>11 = Sum alarm A<br>12 = Sum alarm B<br>13 = Sum alarm C<br>14 = Smoke detector |
| 78 | `VPac1.DO7Ctrl` | X | 2 | Function on DO7:<br>0 = Not activated<br>1 = KAVK<br>2 = ULS<br>3 = Summalarm<br>4 = CP Heating<br>5 = CP Cooling<br>6 = Normal flow<br>7 = Fire damper<br>8 = Not selectable<br>9 = Stove<br>10 = Section hatch (for section exchanger units)<br>11 = Sum alarm A<br>12 = Sum alarm B<br>13 = Sum alarm C<br>14 = Smoke detector |
| 79 | `StdObjs1.PWMCtrl_Period` | I | 60 s | PWM Pulse period |
| 80 | `StdObjs1.ByPassCtrl_RunTime` | I | 120 s | Bypass time from closed to fully opened valve. |
| 81 | `VPac1.NightCool_OutdoorDay` | R | 22 °C | Outdoor mean temperature day must be higher for night cooling |
| 82 | `VPac1.NightCool_Hysteresis` | R | 0.2 °C | Room temperature hysteresis |
| 83 | `VPac1.NightCool_OutdoorLowNight` | R | 5 °C | Outdoor temperature must be higher for night cooling |
| 84 | `VPac1.NightCool_RoomLimit` | R | 18 °C | Room temperature must be higher for start of night cooling |
| 85 | `VPac1.CoolLimit` | R | 2 °C | Cool recycling:<br>Starts if the Outdoor temp. + CoolLimit > Extract Air Temp. (if cool recycling activated) |
| 86 | `VPac1.KAVK_StartTemp` | R | 5 °C | KAVK starts if temp. below this point |
| 87 | `VPac1.KAVK_StopTemp` | R | 7 °C | KAVK stops if temp. above this point |
| 88 | `VPac1.FilterAlarm_Time` | I | 12 | Time in month between filter exchange (Service Alarm) |
| 89 | `VPac1.DefrostTemp` | R | 1 °C | If the exhaust air temp. are below this point during 10 s the defrosting timer activates. |
| 90 | `VPac1.TimeBeforeDefrosting` | R | 1 h | Number of hours before defrosting starts |
| 91 | `VPac1.DefrostTime` | R | 5 min | Defrosting time |
| 92 | `VPac1.SAF_FireAlarm` | R | 0 % | Fire Alarm flow Supply Air Fan (same as 36) |
| 93 | `VPac1.EAF_FireAlarm` | R | 100 % | Fire Alarm flow Exhaust Air Fan (same as 37) |
| 94 | `VPac1.DICtrl6` | X | 1 | Function on DI6:<br>0 = Not activated<br>1 = Min flow<br>2 = Boost flow<br>3 = Max flow<br>4 = Kitchen mode<br>5 = ECO<br>6 = Stove mode<br>7 = Fire Input<br>8 = Fire damper<br>9 = AHU On/Off<br>10 = Normal<br>11 = Filterguard fire<br>12 = Smoke det. error |
| 95 | `VPac1.DO4Ctrl` | X | 3 | Function on DO4:<br>0 = Not activated<br>1 = KAVK<br>2 = ULS<br>3 = Summalarm<br>4 = CP Heating<br>5 = CP Cooling<br>6 = Normal flow<br>7 = Fire damper<br>8 = PWM preheat<br>9 = Stove<br>10 = Section hatch<br>11 = Sum alarm A<br>12 = Sum alarm B<br>13 = Sum alarm C<br>14 = Smoke detector |
| 96 | `VPac1.DO5Ctrl` | X | 4 | Function on DO5:<br>0 = Not activated<br>1 = KAVK<br>2 = ULS<br>3 = Summalarm<br>4 = CP Heating<br>5 = CP Cooling<br>6 = Normal flow<br>7 = Fire damper<br>8 = Not selectable<br>9 = Stove<br>10 = Section hatch<br>11 = Sum alarm A<br>12 = Sum alarm B<br>13 = Sum alarm C<br>14 = Smoke detector |
| 97 | `VPac1.TempDeadband` | R | 1 °C | ECO/ECO2 fan speed deadband |
| 98 | `VPac1.MaxFanSpHeat` | R | 2 °C | ECO/ECO2 Temp diff max fan speed heat |
| 99 | `VPac1.MaxFanSpCool` | R | 4 °C | ECO2 Temp diff max fan speed cool |
| 100 | `VPac1.FFlimitHeat` | R | 1 °C | Tempdiff for increase to boost, heat |
| 101 | `VPac1.FFlimitCool` | R | 1 °C | Tempdiff for increase to boost, cool |
| 102 | `VPac1.FrostRiskExhaustLimit` | R | 0 °C | Preheat setpoint |
| 103 | `StdObjs1.Preheat_PWMhandauto_Select` | X | 2 | Preheat electric mode:<br>0=Manual off<br>1=Manual on<br>2=Auto |
| 104 | `StdObjs1.Preheat_PWMhandauto_ManIn` | R | 0 % | Preheat electric output if manual mode |
| 105 | `VPac1.PGain_Preheat_PID` | R | 33 °C | P-band Preheat air control |
| 106 | `VPac1.ITime_Preheat_PID` | R | 100 s | I-time Preheat air control |
| 107 | `VPac1.RUDispSPchstep` | R | 0.5 °C | ED-RU-DOS setpoint change step |
| 108 | `VPac1.RUDisp_MaxSetpoint` | R | 3 °C | ED-RU setpoint max change |
| 109 | `VPac1.RUDisp_MinSetpoint` | R | -3 °C | ED-RU setpoint min change |
| 110 | `VPac1.HeatingDeadband` | R | 0.5 °C | Deadband on the heatingvalve |
| 111 | `VPac1.HeatingDeadband` | R | 0.5 °C | Deadband on the heatingvalve (same as 110) |
| 112 | `StdObjs1.Timer_StoveOn_Minutes` | R | 15 min | Stove ignite time |
| 113 | `StdObjs1.Timer_StoveOff_Hours` | R | 12 hours | Stove after time |
| 114 | `StdObjs1.Timer_DI_Hours` | R | 0 hours | DI timer |
| 115 | `VPac1.Preheat_FFspeed_offset` | R | 5 | Preheat offset for FF speed |
| 116 | `VPac1.Preheat_Minspeed_offset` | R | -5 | Preheat offset for Min speed |
| 117 | `VPac1.Preheat_PWM_Outdoorlimit` | R | -20 °C | Outdoor limit for electric preheat |
| 118 | `StdObjs1.Preheat_damperhandauto_Select` | X | 2 | Preheat damper mode:<br>0=Manual off<br>1=Manual on<br>2=Auto |
| 119 | `StdObjs1.Preheat_damperhandauto_Man` | R | 0 | Preheat damper output if manual mode |
| 120 | `StdObjs1.Preheat_curve_Y2` | R | 5 | Speed offset for SAF when damper open 25% |
| 121 | `StdObjs1.Preheat_curve_Y3` | R | 10 | Speed offset for SAF when damper open 50% |
| 122 | `StdObjs1.Preheat_curve_Y4` | R | 15 | Speed offset for SAF when damper open 75% |
| 123 | `StdObjs1.Preheat_curve_Y5` | R | 20 | Speed offset for SAF when damper open 100% |
| 124 | `VPac1.HeatType` | X | 1 | Heatbattery type:<br>0 = Water (0-10V)<br>1 = Electric<br>2 = Water (PWM)<br>3 = No heater |
| 125 | `VPac1.MaxSetpoint` | R | 28 | Maximum setting for room/return air setpoint |
| 126 | `VPac1.UAi1_Setting(0)` | X | 1 | UAI1 setting:<br>0=Not active<br>1=Roomsensor<br>2=Frost prot sensor<br>3=TG-R4<br>4=Vent Heater sensor<br>5=Option temp 1<br>6=Option temp 2<br>7=Option temp 3<br>8=Option temp 4<br>9=Temp. after exchanger<br>10=Supply air fan pressure sensor<br>11=Exhaust air fan pressure sensor<br>12=Exchanger pressure drop sensor |
| 127 | `VPac1.UAi2_Setting` | X | 2 | UAI2 setting (same config as UAI1) |
| 128 | `VPac1.UAi3_Setting` | X | 3 | UAI3 setting (same config as UAI1) |
| 129 | `VPac1.UAi4_Setting` | X | 5 | UAI4 setting (same config as UAI1) |
| 130 | `VPac1.DuctHeaterOffset` | R | 0 | Calibrate Duct heater temperature |
| 131 | `VPac1.Aux2Offset` | R | 0 | Calibrate Option temperature |
| 132 | `VPac1.LivingroomSetP` | R | 21 | Living room air setpoint |
| 133 | `VPac1.PGain_Supply_PID_DuctHeat` | R | 33 | P-band supply air control duct heater |
| 134 | `VPac1.PGain_Cascade_PID_DuctHeat` | R | 100 | P-band cascade control duct heater |
| 135 | `VPac1.ITime_Supply_PID_DuctHeat` | R | 100 | I-time supply air control duct heater |
| 136 | `VPac1.ITime_Cascade_PID_DuctHeat` | R | 300 | I-time cascade control duct heater |
| 137 | `StdObjs1.PWMhand_Ductheater_Select` | X | 2 | Duct heater manual control |
| 138 | `VPac1.DuctHeating_Manual` | R | 0 | Duct heater manual output (in %) |
| 139 | `VPac1.SetPAdjDuctheat` | R | 2 | ECO setting for livingroom |
| 140 | `VPac1.ForcedCoolTimeOn_Min` | X | 0 | Forced cool on time (minutes) |
| 141 | `VPac1.ForcedCoolTimeOn_Hour` | X | 0 | Forced cool on time (hour) |
| 142 | `VPac1.ForcedCoolTimeOff_Min` | X | 0 | Forced cool off time (minutes) |
| 143 | `VPac1.ForcedCoolTimeOff_Hour` | X | 24 | Forced cool off time (hour) |
| 144 | `VPac1.ForcedCoolSetP` | R | 28 | Forced cool setpoint |
| 147 | `VPac1.TempAfterExchangerOffset` | R | — | The offset added to temp after exchanger |
| 148 | `VPac1.SAFPressureOffset` | R | — | The offset added to pressure supply air fan |
| 149 | `VPac1.EAFPressureOffset` | R | — | The offset added to pressure exhaust air fan |
| 150 | `VPac1.ExchangerPressureDropOffset` | R | — | The offset added to exchanger pressure drop |
| 151 | `VPac1.SupplyTempReductionAntiFreeze` | R | 2ºC | Supply Temp lowering under freezing protection. (Adjustable 0-10ºC) |
| 152 | `VPac1.LimitOutTempSavePressureDrop` | R | 2ºC | Limit outdoor temp to save pressure values. (Adjustable 0..+10ºC) |
| 153 | `VPac1.MinSupplyTempFreezeProt` | R | 17ºC | Min allowed supply temperature under freeze protection. (Adjustable 0..+25ºC) |
| 154 | `VPac1.StartFrostProtectionOffset` | R | 5ºC | Pressure offset for starting frost protection. (Adjustable 0..100Pa) |
| 155 | `VPac1.FreezeCyclesNormalP` | X | 4 | Number of antifreeze cycles with normal duct pressure. (Adjustable 0..10) |
| 156 | `VPac1.FreezeCyclesReducedP` | X | 4 | Number of antifreeze cycles with reduced duct pressure. (Adjustable 0..10) |
| 157 | `VPac1.MinTimeFreezeSequence` | X | 5 | Min time between freeze cycles |
| 158 | `VPac1.PressureReductionSupply` | R | 10 | Pressure reduction in %,Pa,m³/s,l/s on the supply side under freezing phase 2. |
| 159 | `VPac1.StartTimeFreezeCycle` | I | 5 | Start time before a freeze cycle. (Adjustable 1..60min) |
| 160 | `VPac1.TimeIncreaseFreezeCycle` | I | 1 | Time increase in minutes per freezing cycle. (Adjustable 1..60min) |
| 161 | `VPac1.TotalRunTimeBypass` | I | 2 | Total run-time for the bypass in minutes |
| 162 | `VPac1.MaxOpenDegreeBypass` | I | 100% | Max allowed signal to the bypass damper |
| 163 | `VPac1.FrostRiskOutdoorLimit` | R | 5 °C | Limit in outdoor temperature under which the frostrisk from outdoor is activated |
| 164 | `VPac1.FrostRiskOutdoorOffset` | R | 0 °C | The offset for the outdoor temperature frostrisk |
| 165 | `VPac1.FrostRiskExtractLimit` | R | 2 °C | Limit in extract temperature under which the frostrisk from exhaust is activated |
| 166 | `VPac1.FrostRiskExtractOffset` | R | 1 °C | The offset for the exhaust temperature frostrisk |
| 167 | `VPac1.DefrostStartupDelay` | I | 30 | Delay in seconds before start updating values and can start a defrost |
| 168 | `VPac1.StopAHUDefrostFail` | X | — | Enable/disable the unit if a defrost fail occurs:<br>0=The unit continues to run if fail occurs<br>1=The unit stopped if fail occurs |
| 169 | `VPac1.SAF_VAVPressure_MinSetp` | R | 20 | Min flow Supply Air Fan if pressure control (Pa) |
| 170 | `VPac1.EAF_VAVPressure_MinSetp` | R | 20 | Min flow Exhaust Air Fan if pressure control |
| 171 | `VPac1.SAF_VAVPressure_NormalSetp` | R | 50 | Normal flow Supply Air Fan if pressure control |
| 172 | `VPac1.EAF_VAVPressure_NormalSetp` | R | 50 | Normal flow Exhaust Air Fan if pressure control |
| 173 | `VPac1.SAF_VAVPressure_FFSetp` | R | 80 | Forced flow Supply Air Fan if pressure control |
| 174 | `VPac1.EAF_VAVPressure_FFSetp` | R | 80 | Forced flow Exhaust Air Fan if pressure control |
| 175 | `VPac1.SAF_VAVPressure_MaxSetp` | R | 100 | Max flow Supply Air Fan if pressure control |
| 176 | `VPac1.EAF_VAVPressure_MaxSetp` | R | 100 | Max flow Exhaust Air Fan if pressure control |
| 177 | `VPac1.SAF_VAVPressure_HoodSetp` | R | 80 | Hood flow Supply Air Fan if pressure control |
| 178 | `VPac1.EAF_VAVPressure_HoodSetp` | R | 20 | Hood flow Exhaust Air Fan if pressure control |
| 179 | `VPac1.SAF_VAVPressure_FireSetp` | R | 80 | Fire flow Supply Air Fan if pressure control |
| 180 | `VPac1.EAF_VAVPressure_FireSetp` | R | 20 | Fire flow Exhaust Air Fan if pressure control |
| 181 | `VPac1.SAF_VAVPressure_NightCoolSetp` | R | 50 | Night Cooling flow Supply Air Fan if pressure control |
| 182 | `VPac1.EAF_VAVPressure_NightCoolSetp` | R | 50 | Night Cooling flow Exhaust Air Fan if pressure control |
| 183 | `VPac1.SAF_VAVFlowMinSetp` | R | 20 | Min flow Supply Air Fan if flow control (scale=100) |
| 184 | `VPac1.EAF_VAVFlowMinSetp` | R | 20 | Min flow Exhaust Air Fan if flow control (scale=100) |
| 185 | `VPac1.SAF_VAVFlowNormalSetp` | R | 150 | Normal flow Supply Air Fan if flow control (scale=100) |
| 186 | `VPac1.EAF_VAVFlowNormalSetp` | R | 150 | Normal flow Exhaust Air Fan if flow control (scale=100) |
| 187 | `VPac1.SAF_VAVFlowFFSetp` | R | 80 | Forced flow Supply Air Fan if flow control (scale=100) |
| 188 | `VPac1.EAF_VAVFlowFFSetp` | R | 80 | Forced flow Exhaust Air Fan if flow control (scale=100) |
| 189 | `VPac1.SAF_VAVFlowMaxSetp` | R | 100 | Max flow Supply Air Fan if flow control (scale=100) |
| 190 | `VPac1.EAF_VAVFlowMaxSetp` | R | 100 | Max flow Exhaust Air Fan if flow control (scale=100) |
| 191 | `VPac1.SAF_VAVFlowHoodSetp` | R | 80 | Hood flow Supply Air Fan if flow control (scale=100) |
| 192 | `VPac1.EAF_VAVFlowHoodSetp` | R | 20 | Hood flow Exhaust Air Fan if flow control (scale=100) |
| 193 | `VPac1.SAF_VAVFlowFireSetp` | R | 80 | Fire flow Supply Air Fan if flow control (scale=100) |
| 194 | `VPac1.EAF_VAVFlowFireSetp` | R | 20 | Fire flow Exhaust Air Fan if flow control (scale=100) |
| 195 | `VPac1.SAF_VAVFlowNightCoolSetp` | R | 50 | Night Cooling flow Supply Air Fan if flow control (scale=100) |
| 196 | `VPac1.EAF_VAVFlowNightCoolSetp` | R | 50 | Night Cooling flow Exhaust Air Fan if flow control (scale=100) |
| 197 | `VPac1.FrostRiskOutdoorLowLimit` | R | -12 | Low limit in outdoor temperature under which the frostrisk from outdoor is deactivated |
| 198 | `VPac1.FireIndFuncAckDelay` | I | 30 | Fire indication acknowledge delay, time in minutes to wait to automatic acknowledge the fire indication alarm |
| 201 | `TimeDp.Posts(0).T1` | R | 8 | Start time period 1 Monday (HH.MM) |
| 202 | `TimeDp.Posts(0).T2` | R | 17 | Stop time period 1 Monday |
| 203 | `TimeDp.Posts(0).T3` | R | 0 | Start time period 2 Monday |
| 204 | `TimeDp.Posts(0).T4` | R | 0 | Stop time period 2 Monday |
| 205 | `TimeDp.Posts(1).T1` | R | 8 | Start time period 1 Tuesday |
| 206 | `TimeDp.Posts(1).T2` | R | 17 | Stop time period 1 Tuesday |
| 207 | `TimeDp.Posts(1).T3` | R | 0 | Start time period 2 Tuesday |
| 208 | `TimeDp.Posts(1).T4` | R | 0 | Stop time period 2 Tuesday |
| 209 | `TimeDp.Posts(2).T1` | R | 8 | Start time period 1 Wedn. |
| 210 | `TimeDp.Posts(2).T2` | R | 17 | Stop time period 1 Wedn. |
| 211 | `TimeDp.Posts(2).T3` | R | 0 | Start time period 2 Wedn. |
| 212 | `TimeDp.Posts(2).T4` | R | 0 | Stop time period 2 Wedn. |
| 213 | `TimeDp.Posts(3).T1` | R | 8 | Start time period 1 Thursday |
| 214 | `TimeDp.Posts(3).T2` | R | 17 | Stop time period 1 Thursday |
| 215 | `TimeDp.Posts(3).T3` | R | 0 | Start time period 2 Thursday |
| 216 | `TimeDp.Posts(3).T4` | R | 0 | Stop time period 2 Thursday |
| 217 | `TimeDp.Posts(4).T1` | R | 8 | Start time period 1 Friday |
| 218 | `TimeDp.Posts(4).T2` | R | 17 | Stop time period 1 Friday |
| 219 | `TimeDp.Posts(4).T3` | R | 0 | Start time period 2 Friday |
| 220 | `TimeDp.Posts(4).T4` | R | 0 | Stop time period 2 Friday |
| 221 | `TimeDp.Posts(5).T1` | R | 0 | Start time period 1 Saturday |
| 222 | `TimeDp.Posts(5).T2` | R | 0 | Stop time period 1 Saturday |
| 223 | `TimeDp.Posts(5).T3` | R | 0 | Start time period 2 Saturday |
| 224 | `TimeDp.Posts(5).T4` | R | 0 | Stop time period 2 Saturday |
| 225 | `TimeDp.Posts(6).T1` | R | 0 | Start time period 1 Sunday |
| 226 | `TimeDp.Posts(6).T2` | R | 0 | Stop time period 1 Sunday |
| 227 | `TimeDp.Posts(6).T3` | R | 0 | Start time period 2 Sunday |
| 228 | `TimeDp.Posts(6).T4` | R | 0 | Stop time period 2 Sunday |
| 229 | `TimeDp.Posts(7).T1` | R | 0 | Start time period 1 Holiday |
| 230 | `TimeDp.Posts(7).T2` | R | 0 | Stop time period 1 Holiday |
| 231 | `TimeDp.Posts(7).T3` | R | 0 | Start time period 2 Holiday |
| 232 | `TimeDp.Posts(7).T4` | R | 0 | Stop time period 2 Holiday |
| 301 | `TimeHp.Posts(0).FromDate` | R | 0 | Start date holiday period 1 (MM.DD) |
| 302 | `TimeHp.Posts(0).ToDate` | R | 0 | End date holiday period 1 (MM.DD) |
| 303 | `TimeHp.Posts(1).FromDate` | R | 0 | Start date holiday period 2 (MM.DD) |
| 304 | `TimeHp.Posts(1).ToDate` | R | 0 | End date holiday period 2 (MM.DD) |
| 305 | `TimeHp.Posts(2).FromDate` | R | 0 | Start date holiday period 3 (MM.DD) |
| 306 | `TimeHp.Posts(2).ToDate` | R | 0 | End date holiday period 3 (MM.DD) |
| 307 | `TimeHp.Posts(3).FromDate` | R | 0 | Start date holiday period 4 (MM.DD) |
| 308 | `TimeHp.Posts(3).ToDate` | R | 0 | End date holiday period 4 (MM.DD) |
| 309 | `TimeHp.Posts(4).FromDate` | R | 0 | Start date holiday period 5 (MM.DD) |
| 310 | `TimeHp.Posts(4).ToDate` | R | 0 | End date holiday period 5 (MM.DD) |
| 311 | `TimeHp.Posts(5).FromDate` | R | 0 | Start date holiday period 6 (MM.DD) |
| 312 | `TimeHp.Posts(5).ToDate` | R | 0 | End date holiday period 6 (MM.DD) |
| 313 | `TimeHp.Posts(6).FromDate` | R | 0 | Start date holiday period 7 (MM.DD) |
| 314 | `TimeHp.Posts(6).ToDate` | R | 0 | End date holiday period 7 (MM.DD) |
| 315 | `TimeHp.Posts(7).FromDate` | R | 0 | Start date holiday period 8 (MM.DD) |
| 316 | `TimeHp.Posts(7).ToDate` | R | 0 | End date holiday period 8 (MM.DD) |
| 317 | `TimeHp.Posts(8).FromDate` | R | 0 | Start date holiday period 9 (MM.DD) |
| 318 | `TimeHp.Posts(8).ToDate` | R | 0 | End date holiday period 9 (MM.DD) |
| 319 | `TimeHp.Posts(9).FromDate` | R | 0 | Start date holiday period 10 (MM.DD) |
| 320 | `TimeHp.Posts(9).ToDate` | R | 0 | End date holiday period 10 (MM.DD) |
| 321 | `TimeHp.Posts(10).FromDate` | R | 0 | Start date holiday period 11 (MM.DD) |
| 322 | `TimeHp.Posts(10).ToDate` | R | 0 | End date holiday period 11 (MM.DD) |
| 323 | `TimeHp.Posts(11).FromDate` | R | 0 | Start date holiday period 12 (MM.DD) |
| 324 | `TimeHp.Posts(11).ToDate` | R | 0 | End date holiday period 12 (MM.DD) |
| 325 | `TimeHp.Posts(12).FromDate` | R | 0 | Start date holiday period 13 (MM.DD) |
| 326 | `TimeHp.Posts(12).ToDate` | R | 0 | End date holiday period 13 (MM.DD) |
| 327 | `TimeHp.Posts(13).FromDate` | R | 0 | Start date holiday period 14 (MM.DD) |
| 328 | `TimeHp.Posts(13).ToDate` | R | 0 | End date holiday period 14 (MM.DD) |
| 329 | `TimeHp.Posts(14).FromDate` | R | 0 | Start date holiday period 15 (MM.DD) |
| 330 | `TimeHp.Posts(14).ToDate` | R | 0 | End date holiday period 15 (MM.DD) |
| 331 | `TimeHp.Posts(15).FromDate` | R | 0 | Start date holiday period 16 (MM.DD) |
| 332 | `TimeHp.Posts(15).ToDate` | R | 0 | End date holiday period 16 (MM.DD) |
| 333 | `TimeHp.Posts(16).FromDate` | R | 0 | Start date holiday period 17 (MM.DD) |
| 334 | `TimeHp.Posts(16).ToDate` | R | 0 | End date holiday period 17 (MM.DD) |
| 335 | `TimeHp.Posts(17).FromDate` | R | 0 | Start date holiday period 18 (MM.DD) |
| 336 | `TimeHp.Posts(17).ToDate` | R | 0 | End date holiday period 18 (MM.DD) |
| 337 | `TimeHp.Posts(18).FromDate` | R | 0 | Start date holiday period 19 (MM.DD) |
| 338 | `TimeHp.Posts(18).ToDate` | R | 0 | End date holiday period 19 (MM.DD) |
| 339 | `TimeHp.Posts(19).FromDate` | R | 0 | Start date holiday period 20 (MM.DD) |
| 340 | `TimeHp.Posts(19).ToDate` | R | 0 | End date holiday period 20 (MM.DD) |
| 341 | `TimeHp.Posts(20).FromDate` | R | 0 | Start date holiday period 21 (MM.DD) |
| 342 | `TimeHp.Posts(20).ToDate` | R | 0 | End date holiday period 21 (MM.DD) |
| 343 | `TimeHp.Posts(21).FromDate` | R | 0 | Start date holiday period 22 (MM.DD) |
| 344 | `TimeHp.Posts(21).ToDate` | R | 0 | End date holiday period 22 (MM.DD) |
| 345 | `TimeHp.Posts(22).FromDate` | R | 0 | Start date holiday period 23 (MM.DD) |
| 346 | `TimeHp.Posts(22).ToDate` | R | 0 | End date holiday period 23 (MM.DD) |
| 347 | `TimeHp.Posts(23).FromDate` | R | 0 | Start date holiday period 24 (MM.DD) |
| 348 | `TimeHp.Posts(23).ToDate` | R | 0 | End date holiday period 24 (MM.DD) |
| 350 | `VPac1.HeatValveExDay` | X | 1 | Water heat valve (0-10V) exercise day (0=Nothing, 1=Mon.. 7=Sun) |
| 351 | `VPac1.HeatValveExHour` | X | 0 | Water heat valve (0-10V) exercise hour (0-23) |
| 352 | `VPac1.HeatValveRuntime` | I | 180 | Heat valve run time |
| 353 | `VPac1.BypassOpenOutdoorLimit` | R | 0 | Bypass open outdoor lower limit (if outdoor is under limit bypass can't open) |
| 354 | `VPac1.BypassOpenOutdoorHyst` | R | 1 | Hysteresis |
| 355 | `VPac1.CoolingOutdoorLimit` | R | 20 | Cooling outdoor lower limit (if outdoor is under limit cooling will not output) |
| 356 | `VPac1.CoolingOutdoorHyst` | R | 1 | Hysteresis |
| 401 | `Alarms.AlaAcknow` | X,3 | 255 | External alarm acknowledge by setting this signal to the alarm number that should be acknowledge. |
| 402 | `Alarms.AlaBlock` | X,3 | 255 | External alarm blocking by setting this signal to the alarm number that should be blocked. |
| 403 | `Alarms.AlaUnBlock` | X,3 | 255 | External alarm unblocking by setting this signal to the alarm number that should be unblocked. |
| 411 | `QSystem.Sec` | X,3 | — | Real time clock: Second 0-59 |
| 412 | `QSystem.Minute` | X,3 | — | Real time clock: Minute 0-59 |
| 413 | `QSystem.Hour` | X,3 | — | Real time clock: Hour 0-23 |
| 414 | `QSystem.WDay` | X,3 | — | Real time clock: Day of Week 1-7, 1=Monday |
| 415 | `QSystem.Week` | X,3 | — | Real time clock: Week number 1-53 |
| 416 | `QSystem.Date` | X,3 | — | Real time clock: Day of month 1-31 |
| 417 | `QSystem.Month` | X,3 | — | Real time clock: Month 1-12 |
| 418 | `QSystem.Year` | X,3 | — | Real time clock: Year 0-99 |
| 419 | `VPac1.DO3Ctrl` | X | 0 | Function on DO3:<br>(Only used when HeatType is configured as 0=Water or 3=No heater)<br>0 = Not activated<br>1 = KAVK<br>2 = ULS<br>3 = Summalarm<br>4 = CP Heating<br>5 = CP Cooling<br>6 = Normal flow<br>7 = Fire damper<br>8 = PWM preheat<br>9 = Stove<br>10 = Section hatch<br>11 = Sum alarm A<br>12 = Sum alarm B<br>13 = Sum alarm C<br>14 = Smoke detector |
| 420 | `VPac1.DICtrl7` | X | 0 | Function on DI7:<br>(Only configurable when no electric heater is used)<br>0 = Not activated<br>1 = Min flow<br>2 = Boost flow<br>3 = Max flow<br>4 = Kitchen mode<br>5 = ECO<br>6 = Stove mode<br>7 = Fire Input<br>8 = Fire damper<br>9 = AHU On/Off<br>10 = Normal<br>11 = Filterguard fire<br>12 = Smoke det. error |
| 421 | `VPac1.CO2SensorOffset` | R | 0 | Offset to calibrate CO2 Sensor |
| 422 | `VPac1.HumiditySensorOffset` | R | 0 | Offset to calibrate Humidity sensor |
| 423 | `VPac1.DemandCtrlTemp_SetP` | R | 22 | Demand controlled supply regulation temperature setpoint |
| 424 | `VPac1.DemandCtrlCO2_SetP` | R | 1000 | Demand controlled supply regulation CO2 setpoint |
| 425 | `VPac1.DemandCtrlHum_Setp` | R | 60 | Demand controlled supply regulation humidity setpoint |
| 426 | `VPac1.DemandCtrlTempPID_PGain` | R | 33 | Demand controlled supply regulation temperature PID PGain |
| 427 | `VPac1.DemandCtrlTempPID_ITime` | R | 100 | Demand controlled supply regulation temperature PID ITime |
| 428 | `VPac1.DemandCtrlCO2PID_PGain` | R | 100 | Demand controlled supply regulation CO2 PID PGain |
| 429 | `VPac1.DemandCtrlCO2PID_ITime` | R | 100 | Demand controlled supply regulation CO2 PID ITime |
| 430 | `VPac1.DemandCtrlHumPID_PGain` | R | 33 | Demand controlled supply regulation humidity PID PGain |
| 431 | `VPac1.DemandCtrlHumPID_ITime` | R | 100 | Demand controlled supply regulation humidity PID ITime |
| 432 | `VPac1.StopDefrost_BypassOutWater` | R | 100 | Stop defrost bypass output water heater |
| 433 | `VPac1.StopDefrost_BypassOutElectric` | R | 0 | Stop defrost bypass output electric heater |
| 434 | `VPac1.StopDefrost_SAFspeed` | R | 0 | Stop defrost SAF speed |
| 435 | `VPac1.StopDefrost_EAFspeed` | R | 50 | Stop defrost EAF speed |
| 436 | `VPac1.StopDefrost_WaterHeat` | R | 100 | Stop defrost water heater output |
| 437 | `VPac1.StopDefrost_ElectricHeat` | R | 0 | Stop defrost electric heater output |
