-- A made-up serviceImplementation.sqlite with the same tables as a Savant 12 Mac host.
CREATE TABLE Zones (id INTEGER PRIMARY KEY, name TEXT, type TEXT, serviceID TEXT, logicalComponent TEXT, componentID TEXT, guid TEXT);
CREATE TABLE LightEntities (id INTEGER PRIMARY KEY, zoneID INTEGER, name TEXT, addresses TEXT, entityType TEXT, entitySubType TEXT, technology TEXT, pressCommand TEXT, holdCommand TEXT, releaseCommand TEXT, togglePressCommand TEXT, toggleHoldCommand TEXT, toggleReleaseCommand TEXT, dimmerCommand TEXT, fadeTime INTEGER, delayTime INTEGER, stateName TEXT, lightsAreOn INTEGER, roomLightsControl INTEGER, orderValue INTEGER, isSceneable INTEGER, naturalLightCapable INTEGER, trueImageSupport INTEGER, bleNetworkKey TEXT, bleGroupID TEXT, bleNodeID TEXT, isSavantLoad INTEGER);
CREATE TABLE LightProperties (id INTEGER PRIMARY KEY, lightingEntityID INTEGER, maxKelvinTemp INTEGER, minKelvinTemp INTEGER);
CREATE TABLE ShadeEntities (id INTEGER PRIMARY KEY, zoneID INTEGER, name TEXT, addresses TEXT, entityType TEXT, pressCommand TEXT, holdCommand TEXT, releaseCommand TEXT, togglePressCommand TEXT, toggleHoldCommand TEXT, toggleReleaseCommand TEXT, dimmerCommand TEXT, fadeTime INTEGER, delayTime INTEGER, presetNumber INTEGER, stateName TEXT, sceneNumber INTEGER, orderValue INTEGER, isSceneable INTEGER);
CREATE TABLE DoorLockEntities (id INTEGER PRIMARY KEY, zoneID INTEGER, name TEXT, entityType TEXT, addresses TEXT, lockCommand TEXT, unlockCommand TEXT, lockState TEXT, errorState TEXT, supportsStatus INTEGER, supportsErrors INTEGER, camera TEXT, partner TEXT, authentication TEXT);
CREATE TABLE ServiceImplementationZonedService (id INTEGER PRIMARY KEY, zone TEXT, component TEXT, logicalComponent TEXT, serviceType TEXT, service TEXT, serviceVariantID TEXT, shareMode TEXT, externalZone TEXT, nameOnComponent TEXT, alias TEXT, avioType TEXT);
CREATE TABLE ServiceImplementationRequests (id INTEGER PRIMARY KEY, request TEXT, arguments TEXT, initialState TEXT, finalState TEXT, isCustom INTEGER, isInterService INTEGER);
CREATE TABLE ServiceImplementationRequestMap (id INTEGER PRIMARY KEY, ServiceImplementationZonedService_id INTEGER, ServiceImplementationRequests_id INTEGER);
CREATE TABLE ServiceImplementationServiceResources (id INTEGER PRIMARY KEY, zone TEXT, component TEXT, logicalComponent TEXT, serviceType TEXT, service TEXT, serviceVariantID TEXT, shareMode TEXT, externalZone TEXT, nameOnComponent TEXT, connectorID TEXT, alias TEXT, avioType TEXT, resourceType TEXT, pathOrder INTEGER, avType TEXT, slotID TEXT, moduleName TEXT, mediaType TEXT, logicalIO TEXT, discreteVolume INTEGER, serviceNameAlias TEXT);

INSERT INTO Zones (id, name, type) VALUES (1, 'Den', 'Environmental'), (2, 'Den', 'Environmental'), (3, 'Den', 'User');

INSERT INTO LightEntities (id, zoneID, name, addresses, entityType, entitySubType, technology, pressCommand, releaseCommand, toggleReleaseCommand, dimmerCommand, stateName, orderValue) VALUES
  (1, 1, 'Cove', '001,4,(null),(null),(null),(null)', 'DMX', 'RGBWW', 'Infinite Color', 'DimmerSet', '', '', 'DimmerSet', 'Test Host.Lighting.CurrentColor_4_001', 0),
  (2, 1, 'Lamp', '002,17,/zone/57,(null),(null),(null)', 'DMX', 'RGBW', 'Infinite Color', 'DimmerSet', '', '', 'DimmerSet', 'Test Host.Lighting.CurrentColor_17_002', 1),
  (3, 1, 'Pendants', '002,2,/zone/43,(null),(null),(null)', 'Dimmer', 'W', 'Classic White', 'DimmerSet', '', '', 'DimmerSet', 'Test Host.Lighting.CurrentDimmerLevel_2_002', 2),
  (4, 1, 'Cove Scene', 'Cove Scene,(null),(null),(null),(null),(null)', 'Scene', '', '', '', 'ActivateScene', 'DeactivateScene', '', 'Test Host.Lighting.IsSceneActive_Cove Scene', 3),
  (5, 1, 'Away', '002,1025,/button/37,(null),(null),(null)', 'Scene', '', '', 'ButtonPressAndRelease', '', '', '', 'Test Host.Lighting.CurrentLEDState_1025_002', 4),
  (6, 1, '1', '002,1026,/button/36,(null),(null),(null)', 'Scene', '', '', 'ButtonPressAndRelease', '', '', '', 'Test Host.Lighting.CurrentLEDState_1026_002', 5),
  (7, 1, 'Away', '004,6,(null),(null),(null),(null)', 'Scene', '', '', 'ButtonPress', 'ButtonRelease', 'ButtonRelease', '', 'Test Host.Lighting.CurrentLEDState_6_004', 6);
INSERT INTO LightProperties (lightingEntityID, maxKelvinTemp, minKelvinTemp) VALUES (1, 6000, 2200);

INSERT INTO ShadeEntities (id, zoneID, name, addresses, entityType, pressCommand, releaseCommand, dimmerCommand, stateName, orderValue) VALUES
  (1, 2, 'Sheers', '002,1,/zone/13,(null),(null)', 'Variable', '', 'GroupShadeSet', '', 'Test Host.Lighting.GroupShadeLevel_1_002', 0);

INSERT INTO ServiceImplementationZonedService (id, zone, component, logicalComponent, serviceType, serviceVariantID, alias) VALUES
  (1, 'Den', 'Test Host', 'Lighting', 'SVC_ENV_LIGHTING', '1', 'Test Host'),
  (2, 'Den', 'Test Host', 'Lighting', 'SVC_ENV_SHADE', '1', 'Test Host'),
  (3, 'Den', 'Den Apple TV', 'Media_server', 'SVC_AV_APPLEREMOTEMEDIASERVER', '1', 'Apple TV');

INSERT INTO ServiceImplementationRequests (id, request, arguments, isCustom, isInterService) VALUES
  (1, 'DimmerSet', 'Address1,Address2,Address3,Address4,Address5,Address6,DimmerLevel,FadeTime,DelayTime,bleColorRed,bleColorGreen,bleColorBlue,bleColorWhite,kelvin,Curve', 0, 0),
  (2, 'ActivateScene', 'Address1,Address2,Address3,Address4,Address5,Address6', 0, 0),
  (3, 'DeactivateScene', 'Address1,Address2,Address3,Address4,Address5,Address6', 0, 0),
  (4, 'ButtonPressAndRelease', 'Address1,Address2,Address3,Address4,Address5,Address6', 0, 0),
  (5, 'ButtonPress', 'Address1,Address2,Address3,Address4,Address5,Address6', 0, 0),
  (6, 'ButtonRelease', 'Address1,Address2,Address3,Address4,Address5,Address6', 0, 0),
  (7, '__RoomLightsOff', '', 0, 0),
  (10, 'GroupShadeUp', 'Address1,Address2,Address3,Address4,Address5,Address6', 0, 0),
  (11, 'GroupShadeDown', 'Address1,Address2,Address3,Address4,Address5,Address6', 0, 0),
  (12, 'GroupShadeStop', 'Address1,Address2,Address3,Address4,Address5,Address6', 0, 0),
  (13, 'GroupShadeSet', 'Address1,Address2,Address3,Address4,Address5,Address6,ShadeLevel,FadeTime,DelayTime', 0, 0),
  (14, 'ShadeStop', 'Address1,Address2,Address3,Address4,Address5', 0, 0),
  (15, 'ShadeSet', 'Address1,Address2,Address3,Address4,Address5,Address6,ShadeLevel,FadeTime,DelayTime', 0, 0),
  (20, 'PowerOn', '', 0, 0), (21, 'PowerOff', '', 0, 0), (22, 'Play', '', 0, 0), (23, 'Pause', '', 0, 0),
  (24, 'VolumeUp', '', 0, 0), (25, 'VolumeDown', '', 0, 0), (26, 'MuteOn', '', 0, 0), (27, 'MuteOff', '', 0, 0),
  (28, 'SetVolume', 'VolumeValue', 0, 0), (29, 'Menu', '', 0, 0), (30, 'Home', '', 0, 0),
  (31, 'OSDCursorUp', '', 0, 0), (32, 'OSDCursorDown', '', 0, 0), (33, 'OSDSelect', '', 0, 0),
  (34, 'LaunchApp', 'AppID', 0, 0), (35, 'Den TV_PowerToggle', '', 0, 0),
  (36, 'Netflix', '', 1, 0), (37, 'Youtube', '', 1, 0), (38, 'Apple TV Restart', '', 1, 0),
  (39, 'PowerOffTV', '', 0, 1);

INSERT INTO ServiceImplementationRequestMap (ServiceImplementationZonedService_id, ServiceImplementationRequests_id)
  SELECT 1, id FROM ServiceImplementationRequests WHERE id < 10
  UNION ALL SELECT 2, id FROM ServiceImplementationRequests WHERE id BETWEEN 10 AND 19
  UNION ALL SELECT 3, id FROM ServiceImplementationRequests WHERE id >= 20;

CREATE TABLE CameraEntities (id INTEGER PRIMARY KEY, zoneID INTEGER, name TEXT, componentName TEXT, logicalComponent TEXT, entityType TEXT, previewPath TEXT, fullscreenPath TEXT, previewURL TEXT, fullscreenURL TEXT, highSourceURL TEXT, lowSourceURL TEXT, highMulticastURL TEXT, lowMulticastURL TEXT, previewFormat TEXT, fullscreenFormat TEXT, hostname TEXT, username TEXT, password TEXT, cpassword TEXT, authentication TEXT, port TEXT, transport TEXT, partner TEXT, uriSource TEXT);
INSERT INTO Zones (id, name, type) VALUES (9, 'SAVANT_GLOBAL_ENV_ZONE', 'Environmental');
INSERT INTO CameraEntities (id, zoneID, name, componentName, logicalComponent, entityType, previewURL, fullscreenURL, highSourceURL, lowSourceURL, username, password) VALUES
  (1, 9, 'Porch Camera', 'Porch Camera', 'Security_camera1', 'Fixed', 'http://viewer:secret@10.9.9.2/cgi-bin/snapshot.cgi', 'http://viewer:secret@10.9.9.2/cgi-bin/snapshot.cgi', 'rtsp://viewer:secret@10.9.9.2/cam/realmonitor?channel=1&subtype=0', 'rtsp://viewer:secret@10.9.9.2/cam/realmonitor?channel=1&subtype=1', 'viewer', 'secret'),
  (2, 9, 'Blank Camera', 'Blank Camera', 'Security_camera1', 'Fixed', '', '', '', '', '', '');

CREATE TABLE HVACEntities (id INTEGER PRIMARY KEY, zoneID INTEGER, name TEXT, addresses TEXT, temperatureSetPoints INTEGER, heat INTEGER, cool INTEGER, auto INTEGER, humiditySetPoints INTEGER, humidify INTEGER, dehumidify INTEGER, hasExternalTempSensor INTEGER, history INTEGER, stateName TEXT, tempMinRange INTEGER, tempMaxRange INTEGER, tempBuffer INTEGER, isCelsius INTEGER, homeAway INTEGER, authentication TEXT, partner TEXT);
INSERT INTO HVACEntities (id, zoneID, name, addresses, temperatureSetPoints, heat, cool, auto, humiditySetPoints, stateName, tempMinRange, tempMaxRange, isCelsius) VALUES
  (1, 1, 'Main Floor', '1,(null)', 2, 1, 1, 1, 0, 'Den Tstat.HVAC_controller.ThermostatCurrentTemperature_1', 50, 90, 0);
CREATE TABLE GarageEntities (id INTEGER PRIMARY KEY, zoneID INTEGER, name TEXT, addresses TEXT, entityType TEXT, toggleCommand TEXT, openCommand TEXT, stopCommand TEXT, closeCommand TEXT, openState TEXT, errorState TEXT, supportsErrors INTEGER, supportsStatus INTEGER, supportsStop INTEGER, camera TEXT);
INSERT INTO GarageEntities (id, zoneID, name, addresses, openCommand, stopCommand, closeCommand, openState, errorState) VALUES
  (1, 1, 'Left Door', '2,(null)', 'OpenGarageDoor', 'StopGarageDoor', 'CloseGarageDoor', 'Garage Controller.Garage_controller.GarageDoorStatus_2', 'Garage Controller.Garage_controller.GarageDoorError_2');
CREATE TABLE ZoneConfigComponents (id INTEGER PRIMARY KEY, component TEXT, zone TEXT, model TEXT, manufacturer TEXT, componentType TEXT, componentID TEXT, uid TEXT, zoneType TEXT, deviceType TEXT, hasActions INTEGER, typeID TEXT, internalID TEXT, stationID TEXT);
INSERT INTO ZoneConfigComponents (component, zone, model, manufacturer, deviceType) VALUES
  ('Den Apple TV', 'Den', 'Apple TV 4K', 'Apple', 'Media_server'),
  ('Den TV', 'Den', 'OLED(xx)A1', 'LG', 'HD_monitor'),
  ('Test Host', '[Test Host - Resources]', 'Pro Host', 'Savant', 'RacePointMedia_host');
INSERT INTO ServiceImplementationZonedService (id, zone, component, logicalComponent, serviceType, serviceVariantID, alias) VALUES
  (4, 'Den', 'Den Tstat', 'HVAC_controller', 'SVC_ENV_HVAC', '1', 'Den Tstat'),
  (5, 'Den', 'Garage Controller', 'Garage_controller', 'SVC_ENV_GARAGE', '1', 'Garage Controller');
INSERT INTO ServiceImplementationRequests (id, request, arguments, isCustom, isInterService) VALUES
  (50, 'SetHVACModeOff', 'ThermostatAddress,ThermostatAddress2', 0, 0),
  (51, 'SetHVACModeHeat', 'ThermostatAddress,ThermostatAddress2', 0, 0),
  (52, 'SetHVACModeCool', 'ThermostatAddress,ThermostatAddress2', 0, 0),
  (53, 'SetHVACModeAuto', 'ThermostatAddress,ThermostatAddress2', 0, 0),
  (54, 'SetHeatPointTemperature', 'ThermostatAddress,ThermostatAddress2,HeatPointTemperature', 0, 0),
  (55, 'SetCoolPointTemperature', 'ThermostatAddress,ThermostatAddress2,CoolPointTemperature', 0, 0),
  (56, 'SetFanModeAuto', 'ThermostatAddress,ThermostatAddress2', 0, 0),
  (57, 'SetFanModeOn', 'ThermostatAddress,ThermostatAddress2', 0, 0),
  (60, 'OpenGarageDoor', 'GarageDoorAddress,GarageDoorAddress2', 0, 0),
  (61, 'CloseGarageDoor', 'GarageDoorAddress,GarageDoorAddress2', 0, 0),
  (62, 'StopGarageDoor', 'GarageDoorAddress,GarageDoorAddress2', 0, 0);
INSERT INTO ServiceImplementationRequestMap (ServiceImplementationZonedService_id, ServiceImplementationRequests_id)
  SELECT 4, id FROM ServiceImplementationRequests WHERE id BETWEEN 50 AND 59
  UNION ALL SELECT 5, id FROM ServiceImplementationRequests WHERE id BETWEEN 60 AND 69;

CREATE TABLE EVChargerEntities (id INTEGER PRIMARY KEY, zoneID INTEGER, name TEXT, ipAddress TEXT, address TEXT, startCommand TEXT, stopCommand TEXT, setRateCommand TEXT, linkedPM TEXT, linkedUID TEXT, linkedCircuitId TEXT, circuitId TEXT, chargerState TEXT, errorState TEXT, powerState TEXT, historyState TEXT, chargeRateState TEXT, chargeModeState TEXT, maxChargeRateState TEXT, minChargePowerState TEXT, maxChargePowerState TEXT, currentSessionEnergyState TEXT, currentSessionStartDateState TEXT, lastSessionEnergyState TEXT, lastSessionEndDateState TEXT);
INSERT INTO EVChargerEntities (id, zoneID, name, address, startCommand, stopCommand, chargerState, powerState) VALUES
  (1, 1, 'Driveway Charger', '1', 'StartCharging', 'StopCharging', 'Den Charger.EV_charger.ChargerState_1', 'Den Charger.EV_charger.ChargerPower_1');
CREATE TABLE HotWaterHeaterEntities (id INTEGER PRIMARY KEY, zoneID INTEGER, name TEXT, address TEXT, setTempCommand TEXT, setCapacityCommand TEXT, setModeCommand TEXT, currentSetPointState TEXT, currentCapacityState TEXT, currentModeState TEXT, currentTempState TEXT, timeToSetPointState TEXT, hotWaterRemainState TEXT, partner TEXT, supportsErrors INTEGER);
INSERT INTO HotWaterHeaterEntities (id, zoneID, name, address, setTempCommand, currentSetPointState, currentModeState, currentTempState, hotWaterRemainState) VALUES
  (1, 1, 'Water Heater', '1', 'SetWaterTemperature', 'Den Heater.Water_heater.SetPoint_1', 'Den Heater.Water_heater.Mode_1', 'Den Heater.Water_heater.Temperature_1', 'Den Heater.Water_heater.HotWaterRemaining_1');
INSERT INTO ServiceImplementationZonedService (id, zone, component, logicalComponent, serviceType, serviceVariantID, alias) VALUES
  (6, 'Den', 'Den Charger', 'EV_charger', 'SVC_ENV_EVCHARGER', '1', 'Den Charger'),
  (7, 'Den', 'Den Heater', 'Water_heater', 'SVC_ENV_WATERHEATER', '1', 'Den Heater');
INSERT INTO ServiceImplementationRequests (id, request, arguments, isCustom, isInterService) VALUES
  (70, 'StartCharging', 'Address1', 0, 0), (71, 'StopCharging', 'Address1', 0, 0),
  (80, 'SetWaterTemperature', 'Address1,Temperature', 0, 0);
INSERT INTO ServiceImplementationRequestMap (ServiceImplementationZonedService_id, ServiceImplementationRequests_id) VALUES (6, 70), (6, 71), (7, 80);

CREATE TABLE SecuritySystemEntities (id INTEGER PRIMARY KEY, zoneID INTEGER, name TEXT, componentName TEXT, entityType TEXT, partitionNumber TEXT, zoneNumber TEXT, hasBypass INTEGER, hasLCDFeedback INTEGER, statusState TEXT, bypassToggleState TEXT, bypassTextState TEXT, buttonAssignments TEXT, keypadLabels TEXT, smartPinConfig TEXT, emergencyLabels TEXT, partner TEXT);
INSERT INTO SecuritySystemEntities (id, zoneID, name, componentName, entityType, partitionNumber, zoneNumber, statusState) VALUES
  (1, 9, 'House', 'Alarm Panel', 'Partition', '1', '', 'Alarm Panel.Security_system.CurrentPartitionArmingStatus_1'),
  (2, 9, 'Front Door', 'Alarm Panel', 'Zone', '1', '3', 'Alarm Panel.Security_system.CurrentZoneStatus_3');
CREATE TABLE PoolAndSpaEntities (id INTEGER PRIMARY KEY, zoneID INTEGER, name TEXT, entityType TEXT, auxiliaryNumber TEXT);
INSERT INTO PoolAndSpaEntities (id, zoneID, name, entityType, auxiliaryNumber) VALUES (1, 1, 'Pool Light', 'Auxiliary', '2'), (2, 1, 'Heater', 'Heater', '');
INSERT INTO ServiceImplementationZonedService (id, zone, component, logicalComponent, serviceType, serviceVariantID, alias) VALUES
  (9, 'SAVANT_GLOBAL_ENV_ZONE', 'Alarm Panel', 'Security_system', 'SVC_ENV_SECURITYSYSTEM', '1', 'Alarm Panel'),
  (10, 'Den', 'Pool Controller', 'Pool_controller', 'SVC_ENV_POOLANDSPA', '1', 'Pool Controller');
INSERT INTO ServiceImplementationRequests (id, request, arguments, isCustom, isInterService) VALUES
  (100, 'ArmAlarmAway', 'PartitionNumber,ZoneNumber,User,UserCode', 0, 0),
  (101, 'ArmAlarmStay', 'PartitionNumber,ZoneNumber,User,UserCode', 0, 0),
  (102, 'DisarmAlarm', 'PartitionNumber,ZoneNumber,User,UserCode', 0, 0),
  (110, 'SetAuxiliary2On', '', 0, 0), (111, 'SetAuxiliary2Off', '', 0, 0);
INSERT INTO ServiceImplementationRequestMap (ServiceImplementationZonedService_id, ServiceImplementationRequests_id) VALUES
  (9, 100), (9, 101), (9, 102), (10, 110), (10, 111);
